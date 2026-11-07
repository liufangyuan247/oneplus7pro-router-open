package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStudioOSVersionRequiresExactPublishedManifest(t *testing.T) {
	app := t.TempDir()
	if err := os.Mkdir(filepath.Join(app, "updates"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"release_id":"20261107T060345Z-1725d7e2","working_tree":false}`)
	sum := sha256.Sum256(manifest)
	digest := hex.EncodeToString(sum[:])
	fetch := func(url, accept string) ([]byte, error) {
		switch url {
		case "https://api.github.com/repos/owner/studio/releases/latest":
			return []byte(`{"tag_name":"studioos-v0.4.0","assets":[{"name":"update.json","url":"https://api.github.com/repos/owner/studio/releases/assets/123"}]}`), nil
		case "https://api.github.com/repos/owner/studio/releases/assets/123":
			if accept != "application/octet-stream" {
				t.Fatal("descriptor request must use the asset endpoint")
			}
			return []byte(`{"spec":"studioos.release/v1","release_id":"20261107T060345Z-1725d7e2","manifest_sha256":"` + digest + `"}`), nil
		default:
			t.Fatalf("unexpected metadata request: %s", url)
			return nil, nil
		}
	}
	id, record, err := studioOSPublishedVersionMetadata("owner/studio", fetch)
	if err != nil || record.Version != "v0.4.0" {
		t.Fatalf("formal version not resolved: %#v, %v", record, err)
	}
	cache, _ := json.Marshal(map[string]studioOSPublishedVersion{id: record})
	if err := os.WriteFile(filepath.Join(app, "updates", "admin-release-versions.json"), cache, 0600); err != nil {
		t.Fatal(err)
	}
	if version := studioOSManifestVersion(app, manifest); version != "v0.4.0" {
		t.Fatalf("matching installed artifact has version %q", version)
	}
	modified := []byte(`{"release_id":"20261107T060345Z-1725d7e2","working_tree":true}`)
	if version := studioOSManifestVersion(app, modified); version != "" {
		t.Fatalf("different artifact falsely shown as the latest release: %s", version)
	}
	if version := studioOSManifestVersion(app, []byte(`{"release_id":"20261106T060345Z-aabbccdd"}`)); version != "" {
		t.Fatalf("older installation falsely assigned the latest tag: %s", version)
	}
}

func TestStudioOSRejectsUntrustedReleaseVersionMetadata(t *testing.T) {
	for _, release := range []string{
		`{"tag_name":"studioos-v0.4.0","draft":true}`,
		`{"tag_name":"studioos-v0.4.0","prerelease":true}`,
		`{"tag_name":"other-v0.4.0"}`,
		`{"tag_name":"studioos-v0.4.0","assets":[{"name":"update.json","url":"https://other.example/private"}]}`,
		`{"tag_name":"studioos-v0.4.0","assets":[{"name":"update.json","url":"https://api.github.com/repos/other/repository/releases/assets/123"}]}`,
	} {
		calls := 0
		_, _, err := studioOSPublishedVersionMetadata("owner/studio", func(url, accept string) ([]byte, error) {
			calls++
			return []byte(release), nil
		})
		if err == nil || calls != 1 {
			t.Fatalf("invalid release accepted or fetched an untrusted asset: %s", release)
		}
	}
}
