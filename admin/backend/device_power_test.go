package main

import "testing"

func TestDevicePowerCommand(t *testing.T) {
	tests := []struct {
		action string
		ok     bool
		want   string
	}{
		{action: "unlock", ok: true, want: "/system/bin/input keyevent 224; sleep 0.2; /system/bin/wm dismiss-keyguard; sleep 0.2; /system/bin/input keyevent 3"},
		{action: "lock", ok: true, want: "/system/bin/input keyevent 223"},
		{action: "power", ok: false},
		{action: "", ok: false},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			got, ok := devicePowerCommand(test.action)
			if ok != test.ok || got != test.want {
				t.Fatalf("devicePowerCommand(%q) = (%q, %v), want (%q, %v)", test.action, got, ok, test.want, test.ok)
			}
		})
	}
}
