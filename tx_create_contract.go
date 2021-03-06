package solidity

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"time"

	"github.com/fletaio/common"
	"github.com/fletaio/common/hash"
	"github.com/fletaio/common/util"
	"github.com/fletaio/core/amount"
	"github.com/fletaio/core/data"
	"github.com/fletaio/core/transaction"
	"github.com/fletaio/solidity/vm"
)

var allowedKeyMap = map[common.PublicHash]bool{}

// RegisterAllowedKey is used for allowing the contract creation to the specific key hash
func RegisterAllowedKey(KeyHash common.PublicHash) {
	allowedKeyMap[KeyHash] = true
}

// UnregisterAllowedKey is used for disallowing the contract creation to the specific key hash
func UnregisterAllowedKey(KeyHash common.PublicHash) {
	delete(allowedKeyMap, KeyHash)
}

func init() {
	data.RegisterTransaction("solidity.CreateContract", func(t transaction.Type) transaction.Transaction {
		return &CreateContract{
			Base: transaction.Base{
				Type_: t,
			},
		}
	}, func(loader data.Loader, t transaction.Transaction, signers []common.PublicHash) error {
		tx := t.(*CreateContract)
		if tx.Seq() <= loader.Seq(tx.From()) {
			return ErrInvalidSequence
		}

		if len(signers) > 1 {
			return ErrInvalidSignerCount
		}
		if !allowedKeyMap[signers[0]] {
			return ErrNotAllowed
		}

		fromAcc, err := loader.Account(tx.From())
		if err != nil {
			return err
		}

		if err := loader.Accounter().Validate(loader, fromAcc, signers); err != nil {
			return err
		}
		return nil
	}, func(ctx *data.Context, Fee *amount.Amount, t transaction.Transaction, coord *common.Coordinate) (ret interface{}, rerr error) {
		defer func() {
			if e := recover(); e != nil {
				if err, is := e.(error); is {
					rerr = err
				} else {
					rerr = ErrVirtualMachinePanic
				}
			}
		}()

		tx := t.(*CreateContract)
		sn := ctx.Snapshot()
		defer ctx.Revert(sn)

		if tx.Seq() != ctx.Seq(tx.From())+1 {
			return nil, ErrInvalidSequence
		}
		ctx.AddSeq(tx.From())

		fromAcc, err := ctx.Account(tx.From())
		if err != nil {
			return nil, err
		}
		if err := fromAcc.SubBalance(Fee); err != nil {
			return nil, err
		}

		contAddr := common.NewAddress(coord, 0)
		if is, err := ctx.IsExistAccount(contAddr); err != nil {
			return nil, err
		} else if is {
			return nil, ErrExistAddress
		} else if isn, err := ctx.IsExistAccountName(tx.Name); err != nil {
			return nil, err
		} else if isn {
			return nil, ErrExistAccountName
		}

		statedb := &StateDB{
			Context: ctx,
			Coord:   coord,
		}
		logconfig := &vm.LogConfig{
			DisableMemory: false,
			DisableStack:  false,
			Debug:         false,
		}
		vmCfg := vm.Config{
			Tracer: vm.NewStructLogger(logconfig),
			Debug:  false,
		}
		vctx := vm.Context{
			CanTransfer: CanTransfer,
			Transfer:    Transfer,
			GetHash:     func(uint64) hash.Hash256 { return hash.Hash256{} },
			Origin:      tx.From(),
			BlockNumber: new(big.Int).SetUint64(100),
			Time:        big.NewInt(time.Now().Unix()),
			Difficulty:  new(big.Int),
		}
		evm := vm.NewEVM(vctx, statedb, vmCfg)
		code, err := evm.Create(vm.AccountRef(tx.From()), contAddr, tx.Name, append(tx.Code, tx.Params...), amount.NewCoinAmount(0, 0))
		if err != nil {
			return nil, err
		}
		ctx.Commit(sn)
		return code, nil
	})
}

// CreateContract is a solidity.CreateContract
// It is used to create the new contract
type CreateContract struct {
	transaction.Base
	Seq_   uint64
	From_  common.Address
	Name   string
	Code   []byte
	Params []byte
}

// IsUTXO returns false
func (tx *CreateContract) IsUTXO() bool {
	return false
}

// From returns the creator of the transaction
func (tx *CreateContract) From() common.Address {
	return tx.From_
}

// Seq returns the sequence of the transaction
func (tx *CreateContract) Seq() uint64 {
	return tx.Seq_
}

// Hash returns the hash value of it
func (tx *CreateContract) Hash() hash.Hash256 {
	return hash.DoubleHashByWriterTo(tx)
}

// WriteTo is a serialization function
func (tx *CreateContract) WriteTo(w io.Writer) (int64, error) {
	var wrote int64
	if n, err := tx.Base.WriteTo(w); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	if n, err := util.WriteUint64(w, tx.Seq_); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	if n, err := tx.From_.WriteTo(w); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	if n, err := util.WriteString(w, tx.Name); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	if n, err := util.WriteBytes(w, tx.Code); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	if n, err := util.WriteBytes(w, tx.Params); err != nil {
		return wrote, err
	} else {
		wrote += n
	}
	return wrote, nil
}

// ReadFrom is a deserialization function
func (tx *CreateContract) ReadFrom(r io.Reader) (int64, error) {
	var read int64
	if n, err := tx.Base.ReadFrom(r); err != nil {
		return read, err
	} else {
		read += n
	}
	if v, n, err := util.ReadUint64(r); err != nil {
		return read, err
	} else {
		read += n
		tx.Seq_ = v
	}
	if n, err := tx.From_.ReadFrom(r); err != nil {
		return read, err
	} else {
		read += n
	}
	if v, n, err := util.ReadString(r); err != nil {
		return read, err
	} else {
		read += n
		tx.Name = v
	}
	if bs, n, err := util.ReadBytes(r); err != nil {
		return read, err
	} else {
		read += n
		tx.Code = bs
	}
	if bs, n, err := util.ReadBytes(r); err != nil {
		return read, err
	} else {
		read += n
		tx.Params = bs
	}
	return read, nil
}

// MarshalJSON is a marshaler function
func (tx *CreateContract) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(`{`)
	buffer.WriteString(`"type":`)
	if bs, err := json.Marshal(tx.Type_); err != nil {
		return nil, err
	} else {
		buffer.Write(bs)
	}
	buffer.WriteString(`,`)
	buffer.WriteString(`"timestamp":`)
	if bs, err := json.Marshal(tx.Timestamp_); err != nil {
		return nil, err
	} else {
		buffer.Write(bs)
	}
	buffer.WriteString(`,`)
	buffer.WriteString(`"seq":`)
	if bs, err := json.Marshal(tx.Seq_); err != nil {
		return nil, err
	} else {
		buffer.Write(bs)
	}
	buffer.WriteString(`,`)
	buffer.WriteString(`"from":`)
	if bs, err := tx.From_.MarshalJSON(); err != nil {
		return nil, err
	} else {
		buffer.Write(bs)
	}
	buffer.WriteString(`,`)
	buffer.WriteString(`"code":`)
	if len(tx.Code) == 0 {
		buffer.WriteString(`null`)
	} else {
		buffer.WriteString(`"`)
		buffer.WriteString(hex.EncodeToString(tx.Code))
		buffer.WriteString(`"`)
	}
	buffer.WriteString(`,`)
	buffer.WriteString(`"params":`)
	if len(tx.Params) == 0 {
		buffer.WriteString(`null`)
	} else {
		buffer.WriteString(`"`)
		buffer.WriteString(hex.EncodeToString(tx.Params))
		buffer.WriteString(`"`)
	}
	buffer.WriteString(`}`)
	return buffer.Bytes(), nil
}
