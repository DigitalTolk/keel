package ssh

import "testing"

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Target
		wantErr bool
	}{
		{
			name: "user host and port",
			in:   "bofh@web1#2222",
			want: Target{User: "bofh", Host: "web1", Port: 2222},
		},
		{
			name: "user and host, default port",
			in:   "bofh@web1",
			want: Target{User: "bofh", Host: "web1", Port: 22},
		},
		{
			name: "host only, default port",
			in:   "web1",
			want: Target{User: "", Host: "web1", Port: 22},
		},
		{
			name: "host and port, no user",
			in:   "web1#2200",
			want: Target{User: "", Host: "web1", Port: 2200},
		},
		{
			name: "ipv4 with user and port",
			in:   "root@10.0.0.5#22",
			want: Target{User: "root", Host: "10.0.0.5", Port: 22},
		},
		{
			name:    "empty input is an error",
			in:      "",
			wantErr: true,
		},
		{
			name:    "non-numeric port is an error",
			in:      "bofh@web1#ssh",
			wantErr: true,
		},
		{
			name:    "out-of-range port is an error",
			in:      "bofh@web1#70000",
			wantErr: true,
		},
		{
			name:    "empty host is an error",
			in:      "bofh@#22",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTarget(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseTarget(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTargetAddr(t *testing.T) {
	tgt := Target{User: "bofh", Host: "web1", Port: 2222}
	if got, want := tgt.Addr(), "web1:2222"; got != want {
		t.Fatalf("Addr() = %q, want %q", got, want)
	}
}
