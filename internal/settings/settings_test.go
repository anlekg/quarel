package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentWinsOverFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, map[string]string{"QUAREL_X_NAME": "fichier", "QUAREL_X_EMPTY": ""}); err != nil {
		t.Fatal(err)
	}
	if err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if Get("QUAREL_X_NAME") != "fichier" || Locked("QUAREL_X_NAME") {
		t.Fatalf("file value: %q", Get("QUAREL_X_NAME"))
	}
	t.Setenv("QUAREL_X_NAME", "env")
	if Get("QUAREL_X_NAME") != "env" || !Locked("QUAREL_X_NAME") || FromFile("QUAREL_X_NAME") != "fichier" {
		t.Fatal("environment must win")
	}
	if _, ok := Snapshot()["QUAREL_X_EMPTY"]; ok {
		t.Fatal("empty values are dropped")
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode: %v %v", st.Mode(), err)
	}
}

func TestMissingFile(t *testing.T) {
	if err := Load(t.TempDir()); err != nil || len(Snapshot()) != 0 {
		t.Fatalf("missing file: %v", err)
	}
}
