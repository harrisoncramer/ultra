//go:build integration

package integration

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// redisStore is a Redis instance run as a container, the real secret store the
// custom resolver reads from. Secrets are seeded per scenario as "<app>:<name>".
type redisStore struct {
	cid  string
	addr string
}

// startRedisStore launches a Redis container with a published port and waits
// until it accepts connections on the host, the same path the resolver uses.
func startRedisStore() (*redisStore, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "6379", "redis:7-alpine").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run redis: %w: %s", err, out)
	}
	cid := strings.TrimSpace(string(out))
	s := &redisStore{cid: cid}

	// The published port mapping and the server itself both settle a moment after
	// `docker run` returns, so poll for each and surface the last error on timeout.
	var last error
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if s.addr == "" {
			if addr, err := hostPort(cid); err == nil {
				s.addr = addr
			} else {
				last = err
			}
		}
		if s.addr != "" {
			if err := pingAddr(s.addr); err == nil {
				return s, nil
			} else {
				last = err
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	s.Close()
	return nil, fmt.Errorf("redis did not become ready: %v", last)
}

// hostPort returns the host address the container's 6379 port is published on,
// read from docker inspect so it doesn't depend on `docker port` output format.
func hostPort(cid string) (string, error) {
	const format = `{{(index (index .NetworkSettings.Ports "6379/tcp") 0).HostPort}}`
	out, err := exec.Command("docker", "inspect", "--format", format, cid).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w: %s", err, out)
	}
	port := strings.TrimSpace(string(out))
	if port == "" || port == "<no value>" {
		return "", fmt.Errorf("no host port published yet")
	}
	return "127.0.0.1:" + port, nil
}

// pingAddr dials the published port and issues a RESP PING, so readiness is
// checked over the real connection the resolver will use.
func pingAddr(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return err
	}
	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if !strings.Contains(string(buf[:n]), "PONG") {
		return fmt.Errorf("unexpected ping reply %q", buf[:n])
	}
	return nil
}

// Seed writes an app's secrets into the store.
func (s *redisStore) Seed(app string, secrets map[string]string) error {
	for name, val := range secrets {
		out, err := exec.Command("docker", "exec", s.cid, "redis-cli", "SET", app+":"+name, val).CombinedOutput()
		if err != nil {
			return fmt.Errorf("seeding %s:%s: %w: %s", app, name, err, out)
		}
	}
	return nil
}

// flush clears the store between scenarios so a stale key can't leak across them.
func (s *redisStore) flush() {
	_ = exec.Command("docker", "exec", s.cid, "redis-cli", "FLUSHALL").Run()
}

// addrFlags is the resolver selection and store address passed to every
// store-backed command.
func (s *redisStore) addrFlags() []string {
	return []string{"--secret-resolver", "store", "--store-addr", s.addr}
}

func (s *redisStore) Close() {
	_ = exec.Command("docker", "rm", "-f", s.cid).Run()
}
