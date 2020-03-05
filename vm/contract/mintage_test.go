/*
 * Copyright (c) 2020. QLC Chain Team
 *
 * This software is released under the MIT License.
 * https://opensource.org/licenses/MIT
 */

package contract

import (
	"math/big"
	"testing"
	"time"

	"github.com/qlcchain/go-qlc/common"
	"github.com/qlcchain/go-qlc/common/types"
	"github.com/qlcchain/go-qlc/common/util"
	cfg "github.com/qlcchain/go-qlc/config"
	"github.com/qlcchain/go-qlc/crypto/random"
	"github.com/qlcchain/go-qlc/mock"
	cabi "github.com/qlcchain/go-qlc/vm/contract/abi"
	"github.com/qlcchain/go-qlc/vm/vmstore"
)

func TestMintage(t *testing.T) {
	teardownTestCase, l := setupLedgerForTestCase(t)
	defer teardownTestCase(t)

	ctx := vmstore.NewVMContext(l)

	a := account1.Address()
	b := mock.Address()

	tm, err := l.GetTokenMeta(a, cfg.ChainToken())
	if err != nil {
		t.Fatal(err)
	}

	tokenName := "Test"
	tokenID := cabi.NewTokenHash(a, tm.Header, tokenName)
	nep5TxId := random.RandomHexString(32)

	if data, err := cabi.MintageABI.PackMethod(cabi.MethodNameMintage, tokenID, tokenName,
		"T", big.NewInt(1000), uint8(8), b, nep5TxId); err != nil {
		t.Fatal(err)
	} else {
		m := &Mintage{}
		blk := &types.StateBlock{
			Type:           types.ContractSend,
			Token:          tm.Type,
			Address:        a,
			Balance:        tm.Balance.Sub(types.Balance{Int: MinPledgeAmount}),
			Previous:       tm.Header,
			Link:           types.Hash(types.MintageAddress),
			Representative: tm.Representative,
			Data:           data,
			PoVHeight:      0,
			Timestamp:      common.TimeNow().Unix(),
		}

		if err := m.DoSend(ctx, blk); err != nil {
			t.Fatal(err)
		}

		if key, info, err := m.DoPending(blk); err != nil {
			t.Fatal(err)
		} else {
			t.Log(key, info)
		}

		if receiver, err := m.GetTargetReceiver(ctx, blk); err != nil {
			t.Fatal(err)
		} else if receiver != b {
			t.Fatalf("act: %v, exp: %v", receiver, b)
		}

		rev := &types.StateBlock{}
		if r, err := m.DoReceive(ctx, rev, blk); err != nil {
			t.Fatal(err)
		} else {
			t.Log(r)
		}

		if err := ctx.SaveStorage(); err != nil {
			t.Fatal(err)
		}
		if tokens, err := cabi.ListTokens(ctx); err != nil {
			t.Fatal(err)
		} else {
			t.Log(util.ToIndentString(tokens))
		}
	}

	if data, err := cabi.MintageABI.PackMethod(cabi.MethodNameMintageWithdraw, tokenID); err != nil {
		t.Fatal(err)
	} else {
		blk := &types.StateBlock{
			Type:           types.ContractSend,
			Token:          tm.Type,
			Address:        a,
			Balance:        tm.Balance,
			Vote:           types.ZeroBalance,
			Network:        types.ZeroBalance,
			Storage:        types.ZeroBalance,
			Oracle:         types.ZeroBalance,
			Previous:       tm.Header,
			Link:           types.Hash(types.MintageAddress),
			Representative: tm.Representative,
			Data:           data,
			PoVHeight:      0,
			Timestamp:      common.TimeNow().Unix(),
		}
		wm := WithdrawMintage{}

		// patch withdraw time
		if tokenInfoData, err := ctx.GetStorage(types.MintageAddress[:], tokenID[:]); err != nil {
			t.Fatal(err)
		} else {
			tokenInfo := new(types.TokenInfo)
			err = cabi.MintageABI.UnpackVariable(tokenInfo, cabi.VariableNameToken, tokenInfoData)
			if err != nil {
				t.Fatal(err)
			}

			tokenInfo.WithdrawTime = time.Now().AddDate(0, 0, -1).Unix()
			newTokenInfo, err := cabi.MintageABI.PackVariable(
				cabi.VariableNameToken,
				tokenInfo.TokenId,
				tokenInfo.TokenName,
				tokenInfo.TokenSymbol,
				tokenInfo.TotalSupply,
				tokenInfo.Decimals,
				tokenInfo.Owner,
				tokenInfo.PledgeAmount,
				tokenInfo.WithdrawTime,
				tokenInfo.PledgeAddress,
				tokenInfo.NEP5TxId)
			if err != nil {
				t.Fatal(err)
			}
			if err := ctx.SetStorage(types.MintageAddress[:], tokenID[:], newTokenInfo); err != nil {
				t.Fatal(err)
			}

			if err := ctx.SaveStorage(); err != nil {
				t.Fatal(err)
			}
		}
		if err := wm.DoSend(ctx, blk); err != nil {
			t.Fatal(err)
		}

		if key, info, err := wm.DoPending(blk); err != nil {
			t.Fatal(err)
		} else {
			t.Log(key, info)
		}

		rev := &types.StateBlock{}
		if r, err := wm.DoReceive(ctx, rev, blk); err != nil {
			t.Fatal(err)
		} else {
			t.Log(r)
		}
	}
}

func Test_verifyToken(t *testing.T) {
	nep5TxId := random.RandomHexString(32)

	type args struct {
		param cabi.ParamMintage
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				param: cabi.ParamMintage{
					TokenId:     mock.Hash(),
					TokenName:   "Test",
					TokenSymbol: "t",
					TotalSupply: big.NewInt(100),
					Decimals:    uint8(8),
					Beneficial:  mock.Address(),
					NEP5TxId:    nep5TxId,
				},
			},
			wantErr: false,
		}, {
			name: "f1",
			args: args{
				param: cabi.ParamMintage{
					TokenId:     mock.Hash(),
					TokenName:   "",
					TokenSymbol: "t",
					TotalSupply: big.NewInt(100),
					Decimals:    uint8(8),
					Beneficial:  mock.Address(),
					NEP5TxId:    nep5TxId,
				},
			},
			wantErr: true,
		}, {
			name: "f2",
			args: args{
				param: cabi.ParamMintage{
					TokenId:     mock.Hash(),
					TokenName:   "T**a",
					TokenSymbol: "t",
					TotalSupply: big.NewInt(100),
					Decimals:    uint8(8),
					Beneficial:  mock.Address(),
					NEP5TxId:    nep5TxId,
				},
			},
			wantErr: true,
		}, {
			name: "f3",
			args: args{
				param: cabi.ParamMintage{
					TokenId:     mock.Hash(),
					TokenName:   "Test",
					TokenSymbol: "t&a",
					TotalSupply: big.NewInt(100),
					Decimals:    uint8(8),
					Beneficial:  mock.Address(),
					NEP5TxId:    nep5TxId,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := verifyToken(tt.args.param); (err != nil) != tt.wantErr {
				t.Errorf("verifyToken() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
