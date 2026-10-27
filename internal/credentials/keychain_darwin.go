//go:build darwin && cgo

// Package credentials keeps provider secrets in macOS Keychain, never workspace JSON.
package credentials

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef awayQuery(const char *account) {
 CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
 CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
 CFDictionarySetValue(q, kSecAttrService, CFSTR("ninja.therad.away.providers"));
 CFStringRef a = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
 CFDictionarySetValue(q, kSecAttrAccount, a); CFRelease(a);
 return q;
}
static OSStatus awaySave(const char *account, const char *secret) {
 CFMutableDictionaryRef q = awayQuery(account);
 CFDataRef data = CFDataCreate(NULL, (const UInt8*)secret, strlen(secret));
 const void *keys[] = {kSecValueData}; const void *values[] = {data};
 CFDictionaryRef update = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
 OSStatus status = SecItemUpdate(q, update);
 if (status == errSecItemNotFound) {
  CFDictionarySetValue(q, kSecValueData, data);
  CFDictionarySetValue(q, kSecAttrAccessible, kSecAttrAccessibleWhenUnlockedThisDeviceOnly);
  status = SecItemAdd(q, NULL);
 }
 CFRelease(update); CFRelease(data); CFRelease(q); return status;
}
static OSStatus awayRead(const char *account, void **bytes, long *size, int presenceOnly) {
 CFMutableDictionaryRef q = awayQuery(account);
 CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
 if (!presenceOnly) CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
 CFTypeRef result = NULL;
 OSStatus status = SecItemCopyMatching(q, presenceOnly ? NULL : &result);
 if (status == errSecSuccess && result) {
  *size = CFDataGetLength((CFDataRef)result);
  *bytes = malloc(*size); memcpy(*bytes, CFDataGetBytePtr((CFDataRef)result), *size);
 }
 if (result) CFRelease(result); CFRelease(q); return status;
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

type Keychain struct{}

func (Keychain) Save(provider, key string) error {
	p, k := C.CString(provider), C.CString(key)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(k))
	if C.awaySave(p, k) != C.errSecSuccess {
		return errors.New("macOS Keychain could not save the key. Unlock your login keychain and try again.")
	}
	return nil
}
func (Keychain) Load(provider string) (string, error) {
	p := C.CString(provider)
	defer C.free(unsafe.Pointer(p))
	var data unsafe.Pointer
	var size C.long
	status := C.awayRead(p, &data, &size, 0)
	if status == C.errSecItemNotFound {
		return "", nil
	}
	if status != C.errSecSuccess {
		return "", errors.New("Unlock macOS Keychain to use this provider")
	}
	defer C.free(data)
	return string(C.GoBytes(data, C.int(size))), nil
}
func (Keychain) Has(provider string) bool {
	p := C.CString(provider)
	defer C.free(unsafe.Pointer(p))
	var data unsafe.Pointer
	var size C.long
	return C.awayRead(p, &data, &size, 1) == C.errSecSuccess
}
