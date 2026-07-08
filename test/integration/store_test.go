//go:build integration

package integration

import (
	"fmt"
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

// startRedisStore launches a Redis container with a published port and waits for
// it to accept connections.
func startRedisStore() (*redisStore, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "0:6379", "redis:7-alpine").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run redis: %w: %s", err, out)
	}
	cid := strings.TrimSpace(string(out))
	s := &redisStore{cid: cid}

	// The published port mapping and the server itself both settle a moment after
	// `docker run` returns, so poll for each.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if s.addr == "" {
			if addr, err := publishedAddr(cid); err == nil {
				s.addr = addr
			}
		}
		if s.addr != "" && s.ping() {
			return s, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Close()
	return nil, fmt.Errorf("redis did not become ready")
}

// publishedAddr returns the host address Redis's 6379 port is published on.
func publishedAddr(cid string) (string, error) {
	out, err := exec.Command("docker", "port", cid, "6379/tcp").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port: %w: %s", err, out)
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	i := strings.LastIndex(first, ":")
	if i < 0 {
		return "", fmt.Errorf("unexpected port mapping %q", first)
	}
	return "127.0.0.1:" + first[i+1:], nil
}

func (s *redisStore) ping() bool {
	out, err := exec.Command("docker", "exec", s.cid, "redis-cli", "PING").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) == "PONG"
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
