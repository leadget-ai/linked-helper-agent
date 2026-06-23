package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// reportReq is a minimal valid request body for exercising the transport.
func reportReq() *AccountReportRequest {
	return &AccountReportRequest{SyncedAt: "2026-01-01T00:00:00Z"}
}

func TestBootstrap(t *testing.T) {
	t.Parallel()
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"integrationId":"int-1","reportInterval":300,"enabled":true,` +
			`"knownState":{"accounts":[7],"campaigns":[{"accountId":7,"campaignId":9,"version":3,"hasMessages":true}]}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	resp, err := c.Bootstrap(context.Background(), &BootstrapRequest{
		AgentID: "agent-xyz", AgentVersion: "1.0", Hostname: "h", OS: "linux", PartitionsCount: 2,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if gotPath != "/v1/integrations/linkedin-helper/agent/bootstrap" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotBody, `"agentId":"agent-xyz"`) {
		t.Errorf("request body missing agentId: %s", gotBody)
	}
	if resp.IntegrationID != "int-1" || resp.ReportInterval != 300 || !resp.Enabled {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.KnownState.Accounts) != 1 || len(resp.KnownState.Campaigns) != 1 {
		t.Fatalf("knownState = %+v", resp.KnownState)
	}
	if kc := resp.KnownState.Campaigns[0]; kc.CampaignID != 9 || kc.Version != 3 || !kc.HasMessages {
		t.Errorf("known campaign = %+v", kc)
	}
}

func TestRetriesOn500ThenSucceeds(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) <= 2 { // fail twice, then succeed
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"reportInterval":120}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	resp, err := c.Report(context.Background(), 1, reportReq())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (2 failures + 1 success)", got)
	}
	if resp.ReportInterval != 120 {
		t.Errorf("ReportInterval = %d, want 120", resp.ReportInterval)
	}
}

func TestRetriesOn429(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	if _, err := c.Report(context.Background(), 1, reportReq()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2 (429 is retryable)", got)
	}
}

func TestNoRetryOn400(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	if _, err := c.Report(context.Background(), 1, reportReq()); err == nil {
		t.Fatal("Report: want error on 400, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (4xx is non-retryable)", got)
	}
}

func TestExhaustsRetries(t *testing.T) {
	t.Parallel()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	if _, err := c.Report(context.Background(), 1, reportReq()); err == nil {
		t.Fatal("Report: want error after exhausting retries, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != maxRetries+1 {
		t.Errorf("attempts = %d, want %d (initial + %d retries)", got, maxRetries+1, maxRetries)
	}
}

func TestRequestHeaders(t *testing.T) {
	t.Parallel()
	var auth, contentType, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		accept = r.Header.Get("Accept")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token", true)
	if _, err := c.Report(context.Background(), 1, reportReq()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer secret-token")
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}
}

// TestOversizedResponseTruncated proves a response larger than maxResponseBytes
// still decodes when a complete JSON value sits in the first 1MB: the
// LimitReader cuts the trailing padding, and json.Unmarshal ignores trailing
// whitespace.
func TestOversizedResponseTruncated(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"reportInterval":99}`))
		w.Write([]byte(strings.Repeat(" ", 2<<20))) // >1MB of trailing whitespace
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", true)
	resp, err := c.Report(context.Background(), 1, reportReq())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if resp.ReportInterval != 99 {
		t.Errorf("ReportInterval = %d, want 99", resp.ReportInterval)
	}
}
