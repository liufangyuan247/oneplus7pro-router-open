package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientNameAPIStoresDynamicClientAlias(t *testing.T) {
	server := &Server{data: t.TempDir()}
	request := httptest.NewRequest(http.MethodPut, "/api/clients/name", strings.NewReader(`{"mac":"4E-50-9B-BB-62-13","name":"客厅平板"}`))
	response := httptest.NewRecorder()

	server.clientNameAPI(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", response.Code, response.Body.String())
	}
	if got := server.readClientNames()["4e:50:9b:bb:62:13"]; got != "客厅平板" {
		t.Fatalf("stored name = %q, want 客厅平板", got)
	}

	clients := map[string]ClientInfo{
		"192.168.2.113": {
			IP:        "192.168.2.113",
			MAC:       "4e:50:9b:bb:62:13",
			Hostname:  "未知设备",
			Interface: "eth1",
			Source:    "DHCP",
		},
	}
	applyClientNamesToClients(clients, server.readClientNames())
	client := clients["192.168.2.113"]
	if client.Hostname != "客厅平板" || !client.CustomName {
		t.Fatalf("custom name was not applied: %#v", client)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/clients/name", strings.NewReader(`{"mac":"4e:50:9b:bb:62:13"}`))
	deleteResponse := httptest.NewRecorder()
	server.clientNameAPI(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if len(server.readClientNames()) != 0 {
		t.Fatalf("custom name was not deleted")
	}
}

func TestClientNameAPIRejectsInvalidNames(t *testing.T) {
	server := &Server{data: t.TempDir()}
	for _, body := range []string{
		`{"mac":"4e:50:9b:bb:62:13","name":""}`,
		`{"mac":"4e:50:9b:bb:62:13","name":"bad\nname"}`,
		`{"mac":"invalid","name":"device"}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/clients/name", strings.NewReader(body))
		response := httptest.NewRecorder()
		server.clientNameAPI(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, response.Code)
		}
	}
}
