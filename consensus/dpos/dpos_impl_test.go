package dpos

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/qlcchain/go-qlc/config"
)

func TestGetSeq(t *testing.T) {
	dir := filepath.Join(config.QlcTestDataDir(), "transaction", uuid.New().String())
	cm := config.NewCfgManager(dir)

	dps := NewDPoS(cm.ConfigFile)
	seq1 := dps.getSeq(ackTypeCommon)
	if seq1 != 0 {
		t.Errorf("expect:0   get:%d", seq1)
		t.Fail()
	}

	seq2 := dps.getSeq(ackTypeCommon)
	if seq2 != 1 {
		t.Errorf("expect:1   get:%d", seq2)
		t.Fail()
	}

	seq3 := dps.getSeq(ackTypeFindRep)
	if seq3 != 0x10000002 {
		t.Errorf("expect:%0X   get:%0X", 0x10000002, seq3)
		t.Fail()
	}

	seq4 := dps.getSeq(ackTypeFindRep)
	if seq4 != 0x10000003 {
		t.Errorf("expect:%0X   get:%0X", 0x10000003, seq4)
		t.Fail()
	}
}

func TestGetAckType(t *testing.T) {
	dir := filepath.Join(config.QlcTestDataDir(), "transaction", uuid.New().String())
	cm := config.NewCfgManager(dir)

	dps := NewDPoS(cm.ConfigFile)
	type1 := dps.getAckType(0x10000003)
	if type1 != ackTypeFindRep {
		t.Errorf("expect:%d   get:%d", ackTypeFindRep, type1)
		t.Fail()
	}

	type2 := dps.getAckType(3)
	if type2 != ackTypeCommon {
		t.Errorf("expect:%d   get:%d", ackTypeCommon, type2)
		t.Fail()
	}
}