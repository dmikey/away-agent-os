// away-cloud is the operator-owned control plane. It never runs agent code.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dmikey/away-agent-os/desktop/internal/cloud"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	check := flag.Bool("check-akash", false, "Validate project credentials using a read-only API request")
	listen := flag.String("listen", "127.0.0.1:8789", "Private listen address; put a TLS reverse proxy in front")
	data := flag.String("data", "", "Private durable database path (required)")
	reconcileID := flag.String("reconcile-workspace", "", "Operator-only: reconcile an uncertain record while the service is stopped")
	reconcileSub := flag.String("reconcile-subject", "", "OIDC subject of the workspace owner")
	reconcileDseq := flag.String("reconcile-dseq", "", "Deployment ID manually identified in Akash Console")
	flag.Parse()
	api := &cloud.Akash{Key: os.Getenv("DEREK_AKASH_KEY"), Project: os.Getenv("AWAY_AKASH_PROJECT_ID")}
	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := api.Check(ctx); err != nil {
			return err
		}
		fmt.Println("Akash project credential verified; no resources created.")
		return nil
	}
	if *data == "" {
		return fmt.Errorf("Provide -data outside all agent workspaces")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("Bind to 127.0.0.1 and use an authenticated TLS reverse proxy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	issuer, clientID := os.Getenv("AWAY_OIDC_ISSUER"), os.Getenv("AWAY_OIDC_CLIENT_ID")
	auth, err := cloud.OIDCAuth(ctx, issuer, clientID, strings.Split(os.Getenv("AWAY_CLOUD_ALLOWED_SUBJECTS"), ","))
	cancel()
	if err != nil {
		return err
	}
	store, err := cloud.OpenStore(*data)
	if err != nil {
		return err
	}
	defer store.Close()
	if err = store.Recover(); err != nil {
		return err
	}
	if *reconcileID != "" {
		if *reconcileSub == "" || *reconcileDseq == "" {
			return fmt.Errorf("Reconciliation requires the owner subject and manually verified deployment ID")
		}
		owner := issuer + "\n" + *reconcileSub
		row, err := store.Get(owner, *reconcileID)
		if err != nil {
			return err
		}
		if row.State != "reconcile" || row.Dseq != "" && row.Dseq != *reconcileDseq {
			return fmt.Errorf("Record is not awaiting this deployment's reconciliation")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		state, err := api.State(ctx, *reconcileDseq)
		if err != nil {
			return err
		}
		if _, err = store.Transition(owner, row.ID, "reconcile", state, "Operator manually matched and reconciled this deployment in Akash Console.", *reconcileDseq); err != nil {
			return err
		}
		fmt.Println("Deployment reconciled. Agent execution remains locked.")
		return nil
	}
	image := os.Getenv("AWAY_CLOUD_WORKER_IMAGE")
	enabled := os.Getenv("AWAY_CLOUD_ALLOW_PROVISIONING") == "true"
	if enabled {
		if _, err := cloud.SDL(image); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := api.Check(ctx)
		cancel()
		if err != nil {
			return err
		}
	}
	service := &cloud.Server{Store: store, Akash: api, Config: cloud.Config{Issuer: issuer, ClientID: clientID, LaunchEnabled: enabled, WorkerImage: image}, Authenticate: auth}
	server := &http.Server{Addr: *listen, Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopped)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Print("Away cloud control plane listening; agent execution remains locked pending verified worker attestation")
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
