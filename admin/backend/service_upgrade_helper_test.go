package main

import "testing"

func TestUpgradeHelperMustConfirmActualVersion(t *testing.T) {
	for _, output := range []string{`{"ok":true}`, `{"ok":true,"version":" "}`, `not json`} {
		result, err := parseServiceUpgradeHelperResult([]byte(output))
		if err == nil || result.OK {
			t.Fatalf("unverified helper success accepted: %q", output)
		}
	}
	result, err := parseServiceUpgradeHelperResult([]byte(`{"ok":true,"version":"5.3.0"}`))
	if err != nil || !result.OK || result.Version != "5.3.0" {
		t.Fatalf("confirmed success rejected: %#v, %v", result, err)
	}
}

func TestUpgradeHelperReportsRollbackTruthfully(t *testing.T) {
	for _, restored := range []bool{true, false} {
		output := `{"ok":false,"error":"failed","rolledBack":false}`
		if restored {
			output = `{"ok":false,"error":"failed","rolledBack":true}`
		}
		result, err := parseServiceUpgradeHelperResult([]byte(output))
		if err == nil || result.OK || result.RolledBack != restored || result.Error != "failed" {
			t.Fatalf("rollback status lost: %#v, %v", result, err)
		}
	}
}
