# Minimal Ubuntu sshd for keel's end-to-end bootstrap test.
# Build:  docker build -t keel-sshd -f test/e2e/Dockerfile test/e2e
# Run:    docker run -d --name keel-sshd -p 2222:22 keel-sshd
FROM ubuntu:24.04

# Pre-install the base packages keel provisions so the in-test apt step is fast.
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server sudo python3 python3-apt acl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Permit root password login so the bootstrap connection can authenticate.
RUN mkdir -p /run/sshd \
 && echo 'root:keelpass' | chpasswd \
 && sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config \
 && sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
