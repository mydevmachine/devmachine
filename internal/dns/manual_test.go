package dns

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestManualPrintsTheRecordToCreate(t *testing.T) {
	var out bytes.Buffer
	err := NewManual(&out).Upsert(context.Background(), "example.com",
		Record{Name: "www", Type: "A", Value: "198.51.100.10", TTL: 3600})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"www", "A", "198.51.100.10", "3600", "example.com", "dns status"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the instructions leave out %q:\n%s", want, out.String())
		}
	}
}

func TestManualCannotList(t *testing.T) {
	_, err := NewManual(io.Discard).List(context.Background(), "example.com")
	if err == nil {
		t.Fatal("List passed, but nothing can read a zone the CLI does not speak to")
	}
	if !strings.Contains(err.Error(), "dns status") {
		t.Fatalf("the error does not point at what does work: %v", err)
	}
}

func TestManualSaysHowToStopBeingManual(t *testing.T) {
	var out bytes.Buffer
	_ = NewManual(&out).Upsert(context.Background(), "example.com", Record{Name: "www", Type: "A", Value: "198.51.100.10"})
	if !strings.Contains(out.String(), "packages add") {
		t.Fatalf("it does not mention installing a provider:\n%s", out.String())
	}
}

func TestManualPrintsTheFullNameToRemove(t *testing.T) {
	var out bytes.Buffer
	if err := NewManual(&out).Delete(context.Background(), "app.example.com",
		Record{Name: "@", Type: "A", Value: "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "app.example.com\tA\t203.0.113.10") {
		t.Fatalf("with no provider the zone is a guess, so the full name is printed:\n%s", out.String())
	}
}

func TestManualPrintsTheRecordToAddNextToTheOthers(t *testing.T) {
	var out bytes.Buffer
	err := NewManual(&out).Append(context.Background(), "example.com",
		Record{Name: "@", Type: "MX", Value: "20 mail2.example.com", TTL: 3600})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"example.com\tMX\t20 mail2.example.com\t3600", "keep", "dns status"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the instructions leave out %q:\n%s", want, out.String())
		}
	}
}
