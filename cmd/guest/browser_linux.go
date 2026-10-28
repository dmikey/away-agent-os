//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dmikey/away-agent-os/desktop/internal/guestproto"
	"github.com/mdlayher/vsock"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type browserEngine struct {
	in            *os.File
	reader        *os.File
	out           *bufio.Reader
	cmd           *exec.Cmd
	seq           int
	target        string
	width, height int
}

func startBrowser() (*browserEngine, error) {
	if sharedMounted {
		return nil, errors.New("Browser VM must not have shared folders")
	}
	interfaces, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	for _, n := range interfaces {
		if n.Flags&net.FlagLoopback == 0 {
			return nil, errors.New("Browser VM must not have a network adapter")
		}
	}

	os.MkdirAll("/tmp/browser", 0700)
	os.Chown("/tmp/browser", 2000, 2000)
	os.MkdirAll("/dev/shm", 01777)
	syscall.Mount("tmpfs", "/dev/shm", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, "size=128m,mode=1777")
	// A private pipe, never a debugging port accessible to page scripts or the host UI.
	childRead, hostWrite, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	hostRead, childWrite, e := os.Pipe()
	if e != nil {
		childRead.Close()
		hostWrite.Close()
		return nil, e
	}
	cmd := exec.Command("/usr/lib/chromium/chromium", "--headless=new", "--block-new-web-contents", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-domain-reliability", "--no-pings", "--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--proxy-server=http://127.0.0.1:8080", "--proxy-bypass-list=<-loopback>", "--user-data-dir=/tmp/browser/profile", "--remote-debugging-pipe", "--window-size=1000,700", "about:blank")
	// Only the separately compiled smoke host supplies this boot flag. Production
	// keeps Chromium HTTPS upgrades; the fixture host intercepts explicit HTTP.
	bootArgs, _ := os.ReadFile("/proc/cmdline")
	if strings.Contains(string(bootArgs), "away_browser_fixture=1") {
		cmd.Args = append(cmd.Args, "--disable-features=HttpsUpgrades,HttpsFirstModeV2")
	}

	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp/browser", "TMPDIR=/tmp/browser", "LANG=C.UTF-8", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 2000, Gid: 2000}, Setpgid: true}
	cmd.ExtraFiles = []*os.File{childRead, childWrite}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		childRead.Close()
		childWrite.Close()
		hostRead.Close()
		hostWrite.Close()
		return nil, e
	}
	childRead.Close()
	childWrite.Close()
	ready := false
	defer func() {
		if !ready {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			cmd.Wait()
			hostRead.Close()
			hostWrite.Close()
		}
	}()
	b := &browserEngine{in: hostWrite, reader: hostRead, out: bufio.NewReaderSize(hostRead, 65536), cmd: cmd, width: 1000, height: 700}
	hostRead.SetReadDeadline(time.Now().Add(25 * time.Second))
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if e = b.call("Target.getTargets", nil, "", &targets); e != nil {
		return nil, e
	}
	for _, t := range targets.TargetInfos {
		if t.Type == "page" {
			b.target = t.TargetID
			break
		}
	}
	if b.target == "" {
		return nil, errors.New("Browser page unavailable")
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if e = b.call("Target.attachToTarget", map[string]any{"targetId": b.target, "flatten": true}, "", &attached); e != nil {
		return nil, e
	}
	b.target = attached.SessionID
	if e = b.call("Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}, "", nil); e != nil {
		return nil, e
	}
	if e = b.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 1000, "height": 700, "deviceScaleFactor": 1, "mobile": false}, b.target, nil); e != nil {
		return nil, e
	}
	if e = b.call("Page.enable", nil, b.target, nil); e != nil {
		return nil, e
	}
	if e = b.call("Page.setWebLifecycleState", map[string]any{"state": "active"}, b.target, nil); e != nil {
		return nil, e
	}
	// No new windows: all navigation stays in the one observable surface.
	if e = b.call("Page.setInterceptFileChooserDialog", map[string]any{"enabled": true}, b.target, nil); e != nil {
		return nil, e
	}
	for i := 0; i < 30 && !browserSandboxed(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !browserSandboxed() {
		return nil, errors.New("Chromium renderer sandbox verification failed; rendering denied")
	}
	ready = true
	return b, nil
}
func (b *browserEngine) call(method string, params any, session string, result any) error {
	b.reader.SetReadDeadline(time.Now().Add(35 * time.Second))
	b.seq++
	id := b.seq
	m := map[string]any{"id": id, "method": method, "params": params}
	if params == nil {
		delete(m, "params")
	}
	if session != "" {
		m["sessionId"] = session
	}
	raw, _ := json.Marshal(m)
	if _, e := b.in.Write(append(raw, 0)); e != nil {
		return e
	}
	for {
		var buf bytes.Buffer
		for {
			part, e := b.out.ReadSlice(0)
			if buf.Len()+len(part) > guestproto.BrowserMaxMessage {
				return errors.New("Browser reply limit exceeded")
			}
			buf.Write(part)
			if e == bufio.ErrBufferFull {
				continue
			}
			if e != nil {
				return e
			}
			break
		}
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		data := buf.Bytes()
		if json.Unmarshal(data[:len(data)-1], &reply) != nil {
			return errors.New("Invalid browser protocol")
		}
		if reply.ID != id {
			continue
		}
		if reply.Error != nil {
			return errors.New(reply.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(reply.Result, result)
		}
		return nil
	}
}

// Require both namespace isolation and kernel seccomp filtering in every renderer.
func browserSandboxed() bool {
	dirs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	found := false
	for _, p := range dirs {
		cmd, _ := os.ReadFile(p)
		if !bytes.Contains(cmd, []byte("--type=renderer")) {
			continue
		}
		status, _ := os.ReadFile(filepath.Join(filepath.Dir(p), "status"))
		s := string(status)
		if !strings.Contains(s, "Seccomp:\t2") || !strings.Contains(s, "NoNewPrivs:\t1") || !strings.Contains(s, "Uid:\t2000\t2000\t2000\t2000") {
			return false
		}
		ns, e := os.Readlink(filepath.Join(filepath.Dir(p), "ns/user"))
		host, _ := os.Readlink("/proc/1/ns/user")
		if e != nil || ns == host {
			return false
		}
		found = true
	}
	return found
}
func (b *browserEngine) action(a guestproto.BrowserAction) (guestproto.BrowserFrame, error) {
	if e := guestproto.ValidateBrowser(a); e != nil {
		return guestproto.BrowserFrame{}, e
	}
	call := func(method string, p any) error { return b.call(method, p, b.target, nil) }
	switch a.Kind {
	case "navigate":
		if e := call("Page.navigate", map[string]any{"url": a.URL}); e != nil {
			return guestproto.BrowserFrame{}, e
		}
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
			var ready struct {
				Result struct {
					Value bool `json:"value"`
				} `json:"result"`
			}
			if e := b.call("Runtime.evaluate", map[string]any{"expression": "location.protocol.startsWith('http') && document.readyState !== 'loading' && !!document.body", "returnByValue": true, "timeout": 1000}, b.target, &ready); e == nil && ready.Result.Value {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
	case "reload":
		if e := call("Page.reload", nil); e != nil {
			return guestproto.BrowserFrame{}, e
		}
	case "resize":
		b.width = a.Width
		b.height = a.Height
		if e := call("Emulation.setDeviceMetricsOverride", map[string]any{"width": b.width, "height": b.height, "deviceScaleFactor": 1, "mobile": false}); e != nil {
			return guestproto.BrowserFrame{}, e
		}
	case "click":
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			if e := call("Input.dispatchMouseEvent", map[string]any{"type": typ, "x": a.X, "y": a.Y, "button": "left", "clickCount": 1}); e != nil {
				return guestproto.BrowserFrame{}, e
			}
		}
	case "scroll":
		if e := call("Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": a.X, "y": a.Y, "deltaY": a.Delta, "deltaX": 0}); e != nil {
			return guestproto.BrowserFrame{}, e
		}
	case "type":
		if e := call("Input.insertText", map[string]any{"text": a.Text}); e != nil {
			return guestproto.BrowserFrame{}, e
		}
	case "key":
		codes := map[string]int{"Enter": 13, "Backspace": 8, "Tab": 9, "Escape": 27, "ArrowUp": 38, "ArrowDown": 40, "ArrowLeft": 37, "ArrowRight": 39, "Delete": 46, "Home": 36, "End": 35}
		for _, typ := range []string{"keyDown", "keyUp"} {
			p := map[string]any{"type": typ, "key": a.Key, "windowsVirtualKeyCode": codes[a.Key]}
			if a.Key == "Enter" && typ == "keyDown" {
				p["text"] = "\r"
			}
			if e := call("Input.dispatchKeyEvent", p); e != nil {
				return guestproto.BrowserFrame{}, e
			}
		}
	}
	if !browserSandboxed() {
		return guestproto.BrowserFrame{}, errors.New("Renderer sandbox is not active")
	}
	// Fixed extraction only. User and model cannot supply JavaScript or CDP methods.
	var evaluated struct {
		Result struct {
			Value struct {
				URL      string                      `json:"url"`
				Title    string                      `json:"title"`
				Text     string                      `json:"text"`
				Links    []string                    `json:"links"`
				Controls []guestproto.BrowserControl `json:"controls"`
			} `json:"value"`
		} `json:"result"`
	}
	e := b.call("Runtime.evaluate", map[string]any{"expression": `JSON.parse(JSON.stringify({url:location.href.slice(0,2000),title:document.title.slice(0,300),text:(document.body?.innerText||'').slice(0,30000),controls:Array.from(document.querySelectorAll('a,button,input,textarea,select,[role="button"]')).map(e=>{const r=e.getBoundingClientRect();return {label:(e.getAttribute('aria-label')||e.labels?.[0]?.innerText||e.getAttribute('placeholder')||e.innerText||e.tagName).slice(0,180),tag:e.tagName,x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2),visible:r.width>0&&r.height>0&&r.x>=0&&r.y>=0&&r.bottom<=innerHeight&&r.right<=innerWidth}}).filter(e=>e.visible).slice(0,60).map(({visible,...e})=>e),links:Array.from(document.links).map(a=>a.href).filter(u=>/^https?:/.test(u)).slice(0,40).map(u=>u.slice(0,2000))}))`, "returnByValue": true, "timeout": 2000}, b.target, &evaluated)
	if e != nil {
		return guestproto.BrowserFrame{}, e
	}
	v := evaluated.Result.Value
	r := guestproto.BrowserFrame{Identity: "away-browser-vm/v1", URL: v.URL, Title: v.Title, Text: v.Text, Links: v.Links, Controls: v.Controls, Width: b.width, Height: b.height, Sandboxed: true}
	if a.Kind != "read" {
		var shot struct {
			Data string `json:"data"`
		}
		if e = b.call("Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 65, "captureBeyondViewport": false}, b.target, &shot); e != nil {
			return r, e
		}
		r.JPEG = shot.Data
	}
	return r, nil
}
func browserServer() {
	// Browser VM has no workspace mounts, job execution, or terminal listener.
	l, e := vsock.Listen(guestproto.BrowserPort, nil)
	if e != nil {
		fatal(e)
	}
	var b *browserEngine
	fmt.Println("AWAY_BROWSER_READY isolated/vsock")
	for {
		c, e := l.Accept()
		if e != nil {
			continue
		}
		func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(45 * time.Second))
			var a guestproto.BrowserAction
			var r guestproto.BrowserFrame
			if e := json.NewDecoder(io.LimitReader(c, 8192)).Decode(&a); e != nil {
				r.Error = "Invalid browser request"
			} else if e = guestproto.ValidateBrowser(a); e != nil {
				r.Error = e.Error()
			} else {
				if b == nil {
					b, e = startBrowser()
				}
				if e == nil {
					r, e = b.action(a)
				}
				if e != nil {
					r.Error = e.Error()
					fmt.Fprintln(os.Stderr, "browser:", strconv.Quote(r.Error))
				}
			}
			json.NewEncoder(c).Encode(r)
		}()
	}
}
