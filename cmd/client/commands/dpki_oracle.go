package commands

import (
	"encoding/hex"
	"fmt"

	"github.com/abiosoft/ishell"
	rpc "github.com/qlcchain/jsonrpc2"

	"github.com/qlcchain/go-qlc/cmd/util"
	"github.com/qlcchain/go-qlc/common/types"
	cutil "github.com/qlcchain/go-qlc/common/util"
	"github.com/qlcchain/go-qlc/rpc/api"
)

func addOraclePublishCmdByShell(parentCmd *ishell.Cmd) {
	account := util.Flag{
		Name:  "account",
		Must:  true,
		Usage: "account to publish (private key in hex string)",
		Value: "",
	}
	typ := util.Flag{
		Name:  "type",
		Must:  true,
		Usage: "publish id type (email/weChat)",
		Value: "",
	}
	id := util.Flag{
		Name:  "id",
		Must:  true,
		Usage: "publish id (email address/weChat id)",
		Value: "",
	}
	kt := util.Flag{
		Name:  "kt",
		Must:  true,
		Usage: "publish public key type(ed25519/rsa4096)",
		Value: "",
	}
	pk := util.Flag{
		Name:  "pk",
		Must:  true,
		Usage: "publish public key",
		Value: "",
	}
	code := util.Flag{
		Name:  "code",
		Must:  true,
		Usage: "verification code",
		Value: "",
	}
	hash := util.Flag{
		Name:  "hash",
		Must:  true,
		Usage: "hash verified for",
		Value: "",
	}
	args := []util.Flag{account, typ, id, kt, pk, code, hash}
	c := &ishell.Cmd{
		Name:                "oracle",
		Help:                "oracle publish id and key",
		CompleterWithPrefix: util.OptsCompleter(args),
		Func: func(c *ishell.Context) {
			if util.HelpText(c, args) {
				return
			}

			if err := util.CheckArgs(c, args); err != nil {
				util.Warn(err)
				return
			}

			accountP := util.StringVar(c.Args, account)
			typeP := util.StringVar(c.Args, typ)
			idP := util.StringVar(c.Args, id)
			ktP := util.StringVar(c.Args, kt)
			pkP := util.StringVar(c.Args, pk)
			codeP := util.StringVar(c.Args, code)
			hashP := util.StringVar(c.Args, hash)

			err := oraclePublish(accountP, typeP, idP, ktP, pkP, codeP, hashP)
			if err != nil {
				util.Warn(err)
			}
		},
	}
	parentCmd.AddCmd(c)
}

func oraclePublish(accountP, typeP, idP, ktP, pkP, codeP, hashP string) error {
	if accountP == "" {
		return fmt.Errorf("account can not be null")
	}

	if typeP == "" {
		return fmt.Errorf("publish type can not be null")
	}

	if idP == "" {
		return fmt.Errorf("publish id can not be null")
	}

	if ktP == "" {
		return fmt.Errorf("publish public key type can not be null")
	}

	if pkP == "" {
		return fmt.Errorf("publish public key can not be null")
	}

	if codeP == "" {
		return fmt.Errorf("verification can not be null")
	}

	if hashP == "" {
		return fmt.Errorf("verified hash can not be null")
	}

	accBytes, err := hex.DecodeString(accountP)
	if err != nil {
		return err
	}

	acc := types.NewAccount(accBytes)
	if acc == nil {
		return fmt.Errorf("account format err")
	}

	client, err := rpc.Dial(endpointP)
	if err != nil {
		return err
	}
	defer client.Close()

	param := &api.OracleParam{
		Account: acc.Address(),
		OType:   typeP,
		OID:     idP,
		KeyType: ktP,
		PubKey:  pkP,
		Code:    codeP,
		Hash:    hashP,
	}

	var block types.StateBlock
	err = client.Call(&block, "dpki_getOracleBlock", param)
	if err != nil {
		return err
	}

	var w types.Work
	worker, _ := types.NewWorker(w, block.Root())
	block.Work = worker.NewWork()

	hash := block.GetHash()
	block.Signature = acc.Sign(hash)

	fmt.Printf("oracle block:\n%s\nhash[%s]\n", cutil.ToIndentString(block), block.GetHash())

	var h types.Hash
	err = client.Call(&h, "ledger_process", &block)
	if err != nil {
		return err
	}

	return nil
}
