package backup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
)

// Tool is the "backup" / "restore" / "version" command line shared by the
// Quarel binaries.
type Tool struct {
	Spec      Spec
	MaxSchema int                   // newest database version the program knows
	Identity  func() (string, bool) // the data directory's identity (server ID, issuer), if known
}

// Run executes cmd; handled is false for other commands.
func (t Tool) Run(cmd string, args []string) (handled bool, err error) {
	switch cmd {
	case "backup":
		return true, t.backup(args)
	case "restore":
		return true, t.restore(args)
	case "version":
		fmt.Printf("%s %s — schéma de base v%d\n", t.Spec.Kind, Version(), t.MaxSchema)
		return true, nil
	}
	return false, nil
}

// Version is the build's module version or VCS revision.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(inconnue)"
	}
	v := info.Main.Version
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			v = s.Value[:12]
		}
	}
	return v
}

func (t Tool) backup(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage : %s backup <fichier.tar.gz | ->   (possible pendant que le service tourne)", t.Spec.Kind)
	}
	id, _ := t.Identity()
	var out io.Writer = os.Stdout
	var f *os.File
	if args[0] != "-" {
		var err error
		if f, err = os.OpenFile(args[0], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s existe déjà : choisissez un autre nom (une sauvegarde n'écrase jamais un fichier)", args[0])
		} else if err != nil {
			return err
		}
		out = f
	}
	m, err := Create(t.Spec, id, out)
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(args[0])
		}
	}
	if err != nil {
		return err
	}
	if args[0] != "-" {
		fmt.Fprintf(os.Stderr, "Sauvegarde écrite : %s (%d fichiers, base v%d, %s).\n", args[0], m.Files, m.SchemaVersion, m.Identity)
		fmt.Fprintln(os.Stderr, "Elle contient les clés du service : gardez-la en lieu sûr, idéalement chiffrée.")
	}
	return nil
}

func (t Tool) restore(args []string) error {
	force := false
	var file string
	for _, a := range args {
		if a == "--force" {
			force = true
		} else {
			file = a
		}
	}
	if file == "" {
		return fmt.Errorf("usage : %s restore <fichier.tar.gz | -> [--force]   (service arrêté ; « - » : lire l'entrée standard)", t.Spec.Kind)
	}
	var in io.Reader = os.Stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	m, kept, err := Restore(t.Spec, in, force, t.MaxSchema)
	if err != nil {
		return err
	}
	fmt.Printf("Sauvegarde du %s restaurée dans %s (%d fichiers, base v%d).\n", m.CreatedAt.Local().Format("2006-01-02 15:04"), t.Spec.DataDir, m.Files, m.SchemaVersion)
	if kept != "" {
		fmt.Printf("Les données précédentes ont été mises de côté dans %s.\n", filepath.Base(kept))
	}
	if id, ok := t.Identity(); ok && m.Identity != "" && id != m.Identity {
		return fmt.Errorf("attention : l'identité restaurée (%s) ne correspond pas au manifeste (%s)", id, m.Identity)
	} else if ok {
		fmt.Printf("Identité : %s. Démarrez le service : la base sera mise à jour si besoin.\n", id)
	}
	return nil
}
