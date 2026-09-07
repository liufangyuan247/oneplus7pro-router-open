package main

import (
	"strings"
	"testing"
)

func TestParseSystemLogTextRecognizesStorageDisconnect(t *testing.T) {
	input := `[Mon Sep  7 19:15:29 2026] [20260907_19:15:30.217056]@1 usb 2-1.2: USB disconnect, device number 6
[Mon Sep  7 19:15:33 2026] [20260907_19:15:34.496375]@0 print_req_error: I/O error, dev sdi, sector 0`
	entries := parseSystemLogText("kernel", input)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Time != "2026-09-07 19:15:34" || entries[0].Level != "error" {
		t.Fatalf("unexpected I/O entry: %#v", entries[0])
	}
	if entries[1].Time != "2026-09-07 19:15:30" || entries[1].Level != "warning" {
		t.Fatalf("unexpected disconnect entry: %#v", entries[1])
	}
}

func TestSystemLogRedaction(t *testing.T) {
	message := "GET https://example.test/file?id=123&sign=private token:abcdef password=hunter2"
	redacted := redactSystemLog(message)
	for _, secret := range []string{"id=123", "private", "abcdef", "hunter2"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q remained in %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "查询参数已隐藏") || !strings.Contains(redacted, "[已隐藏]") {
		t.Fatalf("missing redaction markers in %q", redacted)
	}
}

func TestSystemLogLevelDoesNotPromoteRoutineKernelNoise(t *testing.T) {
	if got := systemLogLevel("active wakeup source: a600000.ssusb"); got != "info" {
		t.Fatalf("got %q, want info", got)
	}
	if got := systemLogLevel("power_supply main: driver failed to report `flash_trigger' property: -1"); got != "info" {
		t.Fatalf("got %q, want info", got)
	}
	if got := systemLogLevel("ntfs-3g-mount: mount failed: Device or resource busy"); got != "warning" {
		t.Fatalf("got %q, want warning", got)
	}
}

func TestDeduplicateSystemLogs(t *testing.T) {
	entry := SystemLogEntry{Time: "2026-09-07 19:15:34", Level: "error", Source: "内核日志", Message: "I/O error"}
	got := deduplicateSystemLogs([]SystemLogEntry{entry, entry})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
}
