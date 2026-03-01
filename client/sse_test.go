package client

import (
	"strings"
	"testing"
)

func TestParseSSEResponse_BasicMessage(t *testing.T) {
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("expected ID 1, got %d", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error in response: %v", resp.Error)
	}
}

func TestParseSSEResponse_SkipsNonMessageEvents(t *testing.T) {
	// Heartbeat event followed by actual message
	stream := "event: heartbeat\ndata: {}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"name\":\"test\"}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 2 {
		t.Errorf("expected ID 2, got %d", resp.ID)
	}
}

func TestParseSSEResponse_SkipsMismatchedID(t *testing.T) {
	// First message has wrong ID, second has correct ID
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{\"found\":true}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 5 {
		t.Errorf("expected ID 5, got %d", resp.ID)
	}
}

func TestParseSSEResponse_HandlesErrorResponse(t *testing.T) {
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\"error\":{\"code\":-32600,\"message\":\"Invalid Request\"}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error in response")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("expected error code -32600, got %d", resp.Error.Code)
	}
}

func TestParseSSEResponse_SkipsMalformedData(t *testing.T) {
	// Malformed JSON followed by valid message
	stream := "event: message\ndata: not-json\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":4,\"result\":{}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 4 {
		t.Errorf("expected ID 4, got %d", resp.ID)
	}
}

func TestParseSSEResponse_IgnoresComments(t *testing.T) {
	// SSE comments start with : — should be ignored
	stream := ": this is a comment\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
	resp, err := ParseSSEResponse(strings.NewReader(stream), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("expected ID 1, got %d", resp.ID)
	}
}

func TestParseSSEResponse_StreamEndsWithoutMatch(t *testing.T) {
	// Stream ends without matching ID
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
	_, err := ParseSSEResponse(strings.NewReader(stream), 99)
	if err == nil {
		t.Fatal("expected error when stream ends without matching ID")
	}
}

func TestParseSSEResponse_EmptyStream(t *testing.T) {
	_, err := ParseSSEResponse(strings.NewReader(""), 1)
	if err == nil {
		t.Fatal("expected error on empty stream")
	}
}
