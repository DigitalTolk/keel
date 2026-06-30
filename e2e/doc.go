// Package e2e holds end-to-end tests that provision a real SSH server.
//
// The tests are gated behind the "e2e" build tag and the KEEL_E2E_SSH_ADDR
// environment variable, so they are excluded from the normal unit-test run.
// See test/e2e/Dockerfile and the ci workflow's "e2e" job for how the SSH
// server is built and started.
package e2e
