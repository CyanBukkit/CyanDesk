package deploycheck

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileAndCompose(t *testing.T) {
	docker, err := os.ReadFile("../../deploy/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(docker)
	for _, want := range []string{
		"FROM rust:1.83-bookworm AS gateway",
		"cargo build --release",
		"cyandesk-gateway",
		"FROM golang:1.26-bookworm AS build",
		"FROM node:22-bookworm AS web",
		"EXPOSE 13110 13111",
		"ffmpeg",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
	if strings.Contains(text, "EXPOSE 13112") || strings.Contains(text, "13112:") {
		t.Fatal("Dockerfile publishes the internal port")
	}
	compose, err := os.ReadFile("../../deploy/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c := string(compose)
	for _, want := range []string{"13110:13110/tcp", "13110:13110/udp", "13111:13111", "/usr/local/bin/cyandesk-gateway"} {
		if !strings.Contains(c, want) {
			t.Fatalf("compose missing %q", want)
		}
	}
	if strings.Contains(c, "13112:") {
		t.Fatal("compose publishes the internal redeem port")
	}
	entry, err := os.ReadFile("../../deploy/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	e := string(entry)
	if strings.Contains(e, "cat ") && strings.Contains(e, "bootstrap.token") {
		t.Fatal("entrypoint prints the bootstrap token")
	}
}
