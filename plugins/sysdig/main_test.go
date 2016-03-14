package main

import (
	"testing"
	"time"
)

func TestParseTransaction(t *testing.T) {
	result, err := parseTransaction(`rawtime=1457359147800624128 direction=> pid=30996 method=GET url=/v1.18/containers/a3f6db91513b/json host=/var/run/docker.sock response_code=200 latency=2ms size=255`)
	if err != nil {
		t.Error(err)
	}

	expectedTime := time.Unix(1457359147, 800624128)
	if !result.Timestamp.Equal(expectedTime) {
		t.Errorf("parsed wrong time: expected %v, got %v", expectedTime, result.Timestamp)
	}

	if result.Direction != ">" {
		t.Errorf("parsed wrong Direction: expected %v, got %v", ">", result.Direction)
	}

	if result.PID != "30996" {
		t.Errorf("parsed wrong PID: expected %v, got %v", "30996", result.PID)
	}

	if result.Method != "GET" {
		t.Errorf("parsed wrong Method: expected %v, got %v", "GET", result.Method)
	}

	if result.URL != "/v1.18/containers/a3f6db91513b/json" {
		t.Errorf("parsed wrong URL: expected %v, got %v", "/v1.18/containers/a3f6db91513b/json", result.URL)
	}

	if result.Host != "/var/run/docker.sock" {
		t.Errorf("parsed wrong Host: expected %v, got %v", "/var/run/docker.sock", result.Host)
	}

	if result.ResponseCode != 200 {
		t.Errorf("parsed wrong ResponseCode: expected %v, got %v", 200, result.ResponseCode)
	}

	if result.Latency != 2*time.Millisecond {
		t.Errorf("parsed wrong Latency: expected %v, got %v", 2*time.Millisecond, result.Latency)
	}

	if result.Size != 255 {
		t.Errorf("parsed wrong Size: expected %v, got %v", 255, result.Size)
	}
}
