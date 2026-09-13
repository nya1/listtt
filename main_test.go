package main

import (
	"strings"
	"testing"
)

func TestValidateAddrAcceptsLoopback(t *testing.T) {
	for addr, want := range map[string][2]string{
		"127.0.0.1:7777": {"127.0.0.1", "7777"},
		"localhost:8080": {"localhost", "8080"},
	} {
		host, port, err := validateAddr(addr)
		if err != nil || host != want[0] || port != want[1] {
			t.Errorf("validateAddr(%q) = %q, %q, %v", addr, host, port, err)
		}
	}
}

func TestValidateAddrRejectsOthers(t *testing.T) {
	for _, addr := range []string{
		"0.0.0.0:7777", ":7777", "192.168.1.5:7777", "[::1]:7777", "example.com:7777",
		"127.0.0.1", "127.0.0.1:0", "127.0.0.1:abc", "127.0.0.1:70000",
	} {
		if _, _, err := validateAddr(addr); err == nil {
			t.Errorf("validateAddr(%q) accepted, want an error", addr)
		}
	}
}

func TestRunRejectsNonLoopbackBeforeTouchingStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := run([]string{"--addr", "0.0.0.0:7777"})
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1 or localhost") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsExtraArguments(t *testing.T) {
	if err := run([]string{"extra"}); err == nil {
		t.Fatal("expected an error for unexpected arguments")
	}
}

func TestRunVersionFlag(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("--version err = %v", err)
	}
	if err := run([]string{"-version"}); err != nil {
		t.Fatalf("-version err = %v", err)
	}
}

func TestRunVersionIgnoresOtherFlags(t *testing.T) {
	// GNU-style: --version short-circuits before validation
	if err := run([]string{"--version", "--addr", "0.0.0.0:7777"}); err != nil {
		t.Fatalf("--version with bad addr err = %v", err)
	}
	if err := run([]string{"--version", "extra"}); err != nil {
		t.Fatalf("--version with extra arg err = %v", err)
	}
}
