package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"

	"github.com/cybozu-go/cke"
	"github.com/cybozu-go/well"
	vault "github.com/hashicorp/vault/api"
	"github.com/howeyc/gopass"
	"github.com/spf13/cobra"
)

const (
	ttl100Year  = "876000h"
	ttl10Year   = "87600h"
	approlePath = "approle/"
)

type caParams struct {
	vaultPath  string
	commonName string
	key        string
}

var (
	cas = []caParams{
		{
			vaultPath:  "cke/ca-server/",
			commonName: "server CA",
			key:        "server",
		},
		{

			vaultPath:  "cke/ca-etcd-peer/",
			commonName: "etcd peer CA",
			key:        "etcd-peer",
		},
		{
			vaultPath:  "cke/ca-etcd-client/",
			commonName: "etcd client CA",
			key:        "etcd-client",
		},
		{
			vaultPath:  "cke/ca-kubernetes/",
			commonName: "kubernetes CA",
			key:        "kubernetes",
		},
	}

	ckePolicy = `
path "cke/*"
{
  capabilities = ["create", "read", "update", "delete", "list", "sudo"]
}`
)

func connectVault(ctx context.Context) (*vault.Client, error) {
	cfg := vault.DefaultConfig()
	vc, err := vault.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	if vc.Token() != "" {
		return vc, nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Vault username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	username = username[0 : len(username)-1]
	pass, err := gopass.GetPasswdPrompt("Vault password: ", false, os.Stdin, os.Stdout)
	if err != nil {
		return nil, err
	}
	password := string(pass)

	secret, err := vc.Logical().Write("/auth/userpass/login/"+username,
		map[string]interface{}{"password": password})
	if err != nil {
		return nil, err
	}
	vc.SetToken(secret.Auth.ClientToken)

	return vc, nil
}

func initVault(ctx context.Context) error {
	vc, err := connectVault(ctx)
	if err != nil {
		return err
	}

	for _, ca := range cas {
		err = createCA(ctx, vc, ca)
		if err != nil {
			return err
		}
	}

	found := false
	auths, err := vc.Sys().ListAuth()
	if err != nil {
		return err
	}
	for k := range auths {
		if k == approlePath {
			found = true
			break
		}
	}
	if !found {
		err = vc.Sys().EnableAuthWithOptions(approlePath, &vault.EnableAuthOptions{
			Type: "approle",
		})
		if err != nil {
			return err
		}
	}

	err = vc.Sys().PutPolicy("cke", ckePolicy)
	if err != nil {
		return err
	}

	_, err = vc.Logical().Write("auth/approle/role/cke", map[string]interface{}{
		"policies": "cke",
		"period":   "1h",
	})
	if err != nil {
		return err
	}

	secret, err := vc.Logical().Read("auth/approle/role/cke/role-id")
	if err != nil {
		return err
	}
	roleID := secret.Data["role_id"].(string)

	secret, err = vc.Logical().Write("auth/approle/role/cke/secret-id", map[string]interface{}{})
	if err != nil {
		return err
	}
	secretID := secret.Data["secret_id"].(string)

	cfg := new(cke.VaultConfig)
	cfg.Endpoint = vc.Address()
	cfg.RoleID = roleID
	cfg.SecretID = secretID

	err = storage.PutVaultConfig(ctx, cfg)
	if err != nil {
		return err
	}

	return nil
}

func createCA(ctx context.Context, vc *vault.Client, ca caParams) error {
	mounted := false
	mounts, err := vc.Sys().ListMounts()
	if err != nil {
		return err
	}
	for k := range mounts {
		if k == ca.vaultPath {
			mounted = true
			break
		}
	}
	if !mounted {
		err = vc.Sys().Mount(ca.vaultPath, &vault.MountInput{
			Type: "pki",
			Config: vault.MountConfigInput{
				MaxLeaseTTL:     ttl100Year,
				DefaultLeaseTTL: ttl10Year,
			},
		})
		if err != nil {
			return err
		}
	}

	secret, err := vc.Logical().Write(path.Join(ca.vaultPath, "/root/generate/internal"), map[string]interface{}{
		"common_name": ca.commonName,
		"ttl":         ttl100Year,
		"format":      "pem",
	})
	if err != nil {
		return err
	}
	_, ok := secret.Data["certificate"]
	if !ok {
		return fmt.Errorf("Failed to issue ca: %#v", secret.Warnings)
	}
	return storage.PutCACertificate(ctx, ca.key, secret.Data["certificate"].(string))
}

// vaultInitCmd represents the "vault init" command
var vaultInitCmd = &cobra.Command{
	Use:   "init",
	Short: "configure Vault for CKE",
	Long: `Configure HashiCorp Vault for CKE.

Vault will be configured to:

    * have "cke" policy that can use secrets under cke/.
    * have "ca-server", "ca-etcd-peer", "ca-etcd-client", "ca-kubernetes"
      PKI secrets under cke/.
    * creates AppRole for CKE.

This command will ask username and password for Vault authentication
when VAULT_TOKEN environment variable is not set.`,

	RunE: func(cmd *cobra.Command, args []string) error {
		well.Go(initVault)
		well.Stop()
		return well.Wait()
	},
}

func init() {
	vaultCmd.AddCommand(vaultInitCmd)
}