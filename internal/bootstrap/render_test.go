package bootstrap

import "testing"

func TestInventoryLineWithoutPassword(t *testing.T) {
	h := InventoryHost{Host: "web1", Port: 22, User: "bofh"}
	want := "[web1]:22 ansible_user=bofh ansible_python_interpreter=/usr/bin/python3"
	if got := h.Line(); got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

func TestInventoryLineWithPassword(t *testing.T) {
	h := InventoryHost{Host: "web1", Port: 2222, User: "root", Password: "s3cr3t"}
	want := "[web1]:2222 ansible_user=root ansible_password=s3cr3t ansible_become_password=s3cr3t ansible_python_interpreter=/usr/bin/python3"
	if got := h.Line(); got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

func TestInventoryLineCustomPythonInterpreter(t *testing.T) {
	h := InventoryHost{Host: "h", Port: 22, User: "bofh", PythonInterpreter: "/usr/bin/python3.11"}
	want := "[h]:22 ansible_user=bofh ansible_python_interpreter=/usr/bin/python3.11"
	if got := h.Line(); got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

func TestSudoersContent(t *testing.T) {
	want := "bofh ALL=(ALL) NOPASSWD:ALL\nDefaults:bofh !requiretty\n"
	if got := SudoersContent("bofh"); got != want {
		t.Fatalf("SudoersContent() = %q, want %q", got, want)
	}
}
