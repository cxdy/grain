package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/secrets"
	"github.com/spf13/cobra"
)

func cmdSecret(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "secret",
		Short: "Host secrets store (inject into VMs)",
		Long: `Manage host-side secrets under ~/.grain/secrets and inject them into VMs.

  grain secret set NAME --from-file PATH
  grain secret set NAME --value 'plaintext'
  grain secret ls
  grain secret rm NAME
  grain secret inject [vm] NAME

Injected secrets are written in the guest to /run/grain/secrets/NAME (mode 0600)
unless --path is set.`,
	}
	root.AddCommand(cmdSecretLs(cfgPath))
	root.AddCommand(cmdSecretSet(cfgPath))
	root.AddCommand(cmdSecretRm(cfgPath))
	root.AddCommand(cmdSecretInject(cfgPath))
	return root
}

func cmdSecretLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List host secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			list, err := c.ListSecrets(ctx)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("no secrets — create one:  grain secret set NAME --value '…'")
				return nil
			}
			fmt.Printf("%-24s %-8s %8s  %s\n", "NAME", "MODE", "SIZE", "UPDATED")
			for _, m := range list {
				mode := m.Mode
				if mode == "" {
					mode = "0600"
				}
				fmt.Printf("%-24s %-8s %8d  %s\n", m.Name, mode, m.Size, m.UpdatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func cmdSecretSet(cfgPath *string) *cobra.Command {
	var fromFile, value, mode string
	cmd := &cobra.Command{
		Use:   "set NAME",
		Short: "Create or replace a host secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !secrets.ValidName(name) {
				return fmt.Errorf("invalid secret name %q", name)
			}
			var data []byte
			switch {
			case fromFile != "" && value != "":
				return fmt.Errorf("use only one of --from-file or --value")
			case fromFile != "":
				b, err := os.ReadFile(fromFile)
				if err != nil {
					return err
				}
				data = b
			case value != "":
				data = []byte(value)
			default:
				return fmt.Errorf("provide --from-file PATH or --value TEXT")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			m, err := c.SetSecret(ctx, secrets.PutRequest{
				Name:       name,
				DataBase64: base64.StdEncoding.EncodeToString(data),
				Mode:       mode,
			})
			if err != nil {
				return err
			}
			fmt.Printf("secret %s  size=%d mode=%s\n", m.Name, m.Size, m.Mode)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "read secret bytes from file")
	cmd.Flags().StringVar(&value, "value", "", "secret plaintext value")
	cmd.Flags().StringVar(&mode, "mode", "0600", "file mode when materialised in guest (octal)")
	return cmd
}

func cmdSecretRm(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Delete a host secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			if err := c.DeleteSecret(ctx, args[0]); err != nil {
				return err
			}
			fmt.Println("deleted secret", args[0])
			return nil
		},
	}
}

func cmdSecretInject(cfgPath *string) *cobra.Command {
	var guestPath string
	cmd := &cobra.Command{
		Use:   "inject [vm] NAME",
		Short: "Materialize a host secret into a running VM",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			var vmName, secretName string
			if len(args) == 1 {
				secretName = args[0]
				vmName, err = resolveVMName(c, nil, false)
				if err != nil {
					return err
				}
			} else {
				vmName = args[0]
				secretName = args[1]
			}
			out, err := c.InjectSecret(ctx, vmName, secretName, guestPath)
			if err != nil {
				return err
			}
			path := out.Path
			if path == "" {
				path = "/run/grain/secrets/" + secretName
			}
			fmt.Printf("injected %s → %s:%s mode=%s\n", secretName, vmName, path, out.Mode)
			return nil
		},
	}
	cmd.Flags().StringVar(&guestPath, "path", "", "guest path (default /run/grain/secrets/NAME)")
	return cmd
}

// parsePublishSocketFlag parses HOSTPATH:GUESTPATH for --publish-socket.
func parsePublishSocketFlag(s string) (host, guest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty --publish-socket value")
	}
	// Split on last colon so host paths with drive letters (unlikely on Unix) are safer;
	// guest path must be absolute Unix.
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", fmt.Errorf("invalid --publish-socket %q (want HOSTPATH:GUESTPATH)", s)
	}
	host = s[:i]
	guest = s[i+1:]
	if !strings.HasPrefix(guest, "/") {
		return "", "", fmt.Errorf("guest path must be absolute: %q", guest)
	}
	if host == "" {
		return "", "", fmt.Errorf("host path is required")
	}
	return host, guest, nil
}

func parsePublishSocketFlags(vals []string) ([]struct{ Host, Guest string }, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := make([]struct{ Host, Guest string }, 0, len(vals))
	for _, v := range vals {
		h, g, err := parsePublishSocketFlag(v)
		if err != nil {
			return nil, err
		}
		out = append(out, struct{ Host, Guest string }{h, g})
	}
	return out, nil
}
