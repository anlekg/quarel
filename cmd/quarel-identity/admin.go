package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/anlekg/quarel/internal/identity"
)

const adminUsage = `quarel-identity admin — outils de l'opérateur du service Identity
(à lancer sur la machine du service, avec les mêmes variables QUAREL_* ; ex. docker exec).

  quarel-identity admin disable <pseudo|email|id> --reason "réquisition n°…" [--by NOM]
        désactive un compte (sur ordre juridique) : connexion, jetons et profil refusés,
        connexions en cours fermées, serveurs communautaires prévenus ; ses appareils
        et données sont conservés (retrouvés s'il est réactivé)
  quarel-identity admin enable <pseudo|email|id> --reason "…" [--by NOM]
        réactive un compte
  quarel-identity admin log [nombre]
        journal des actions de l'opérateur
  quarel-identity admin rotate-signing-key --reason "…" [--by NOM]
        remplace la clé de signature des jetons (redémarrer le service ensuite) ;
        l'ancienne clé publique reste publiée le temps que ses jetons expirent
`

func admin(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" {
		fmt.Print(adminUsage)
		return nil
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	reason := fs.String("reason", "", "raison / référence de la réquisition")
	by := fs.String("by", operatorName(), "nom de l'opérateur")
	// Flags may follow the positional argument.
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	cfg, err := identity.ConfigFromEnv()
	if err != nil {
		return err
	}
	db, err := identity.OpenDB(filepath.Join(cfg.DataDir, "identity.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	a := identity.NewAdmin(db)
	ctx := context.Background()
	switch cmd {
	case "disable", "enable":
		if len(positional) != 1 {
			return fmt.Errorf("usage : quarel-identity admin %s <pseudo|email|id> --reason \"…\"", cmd)
		}
		pseudo, err := a.SetDisabled(ctx, positional[0], cmd == "disable", *reason, *by)
		if err != nil {
			return err
		}
		if cmd == "disable" {
			fmt.Printf("Compte %s désactivé (consigné). Ses connexions en cours seront fermées sous 15 s ; les serveurs communautaires l'apprendront sous 10 min.\n", pseudo)
		} else {
			fmt.Printf("Compte %s réactivé (consigné).\n", pseudo)
		}
	case "log":
		n := 50
		if len(positional) > 0 {
			fmt.Sscan(positional[0], &n)
		}
		entries, err := a.Log(ctx, n)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("Journal vide.")
		}
		for _, e := range entries {
			fmt.Printf("%s  %-20s %-26s par %s — %s\n", e.At.Local().Format("2006-01-02 15:04"), e.Action, e.UserID, e.Operator, e.Reason)
		}
	case "rotate-signing-key":
		old, fresh, err := a.RotateSigningKey(ctx, cfg.DataDir, *reason, *by)
		if err != nil {
			return err
		}
		fmt.Printf("Clé de signature remplacée : %s → %s.\nRedémarrez le service pour l'utiliser. L'ancienne clé publique reste publiée %s de plus.\n",
			old, fresh, cfg.TokenTTL)
	default:
		fmt.Fprint(os.Stderr, adminUsage)
		return fmt.Errorf("commande inconnue %q", cmd)
	}
	return nil
}

func operatorName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return strings.TrimSpace(os.Getenv("USER"))
}
