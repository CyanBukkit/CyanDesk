package netlab

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestEmulatedUserspaceNAT rewrites a private UDP source to a mapped address
// inside a fresh network namespace. The forwarder is userspace. It is not a
// public NAT and it is not the routed 10.1/10.2 lab.
func TestEmulatedUserspaceNAT(t *testing.T) {
	if os.Getenv("CYANDESK_NAT_INNER") != "1" {
		cmd := exec.Command("unshare", "-r", "-n", os.Args[0], "-test.run", "^TestEmulatedUserspaceNAT$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CYANDESK_NAT_INNER=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := exec.Command("ip", "link", "set", "lo", "up").Run(); err != nil {
		t.Fatal(err)
	}
	kernelNAT := exec.Command("iptables", "-t", "nat", "-L").Run() == nil
	if kernelNAT {
		// Do not install a rule. Presence of the table is not a public NAT.
	}
	host, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	ctrl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	mapped, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer mapped.Close()
	go forwardMapped(mapped, ctrl.LocalAddr().(*net.UDPAddr))

	if _, err := host.WriteToUDP([]byte("punch"), mapped.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = ctrl.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, from, err := ctrl.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "punch" {
		t.Fatalf("payload %q", buf[:n])
	}
	hostAddr := host.LocalAddr().(*net.UDPAddr)
	if from.Port == hostAddr.Port {
		t.Fatal("controller saw the private host port; the rewrite did not happen")
	}
	if _, err := ctrl.WriteToUDP([]byte("pong"), from); err != nil {
		t.Fatal(err)
	}
	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = host.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "pong" {
		t.Fatalf("return payload %q", buf[:n])
	}
	raw, err := json.Marshal(map[string]any{
		"kind":                          "emulated-userspace-nat",
		"kernel_nat_table_visible":      kernelNAT,
		"kernel_nat_rule_installed":     false,
		"public_nat":                    false,
		"private_routed_lab":            false,
		"mapped_port_differs_from_host": true,
		"return_path_ok":                true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("/opt/cursor/artifacts/cyandesk", 0o755); err == nil {
		_ = os.WriteFile("/opt/cursor/artifacts/cyandesk/emulated-nat.json", append(raw, '\n'), 0o644)
	}
}

func forwardMapped(mapped *net.UDPConn, dest *net.UDPAddr) {
	out, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return
	}
	defer out.Close()
	var client *net.UDPAddr
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, err := out.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if client == nil {
				continue
			}
			_, _ = mapped.WriteToUDP(buf[:n], client)
			close(done)
			return
		}
	}()
	buf := make([]byte, 2048)
	n, from, err := mapped.ReadFromUDP(buf)
	if err != nil {
		return
	}
	client = from
	_, _ = out.WriteToUDP(buf[:n], dest)
	<-done
}
