package native

import (
	"encoding/json"
	"errors"
	"hash"
	"strconv"
	"strings"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/eth/tracers"
	"golang.org/x/crypto/sha3"
)

func init() {
	register("openchainTracer", newOpenchainTracer)
}

type anyRecord interface {
}

type sloadRecord struct {
	Path  string         `json:"path"`
	Slot  libcommon.Hash `json:"slot"`
	Type  string         `json:"type"`
	Value libcommon.Hash `json:"value"`
}

type sstoreRecord struct {
	Path     string         `json:"path"`
	Slot     libcommon.Hash `json:"slot"`
	Type     string         `json:"type"`
	NewValue libcommon.Hash `json:"newValue"`
	OldValue libcommon.Hash `json:"oldValue"`
}

type logRecord struct {
	Path   string           `json:"path"`
	Data   hexutility.Bytes `json:"data"`
	Topics []libcommon.Hash `json:"topics"`
	Type   string           `json:"type"`
}

type callRecord struct {
	Path         string            `json:"path"`
	Children     []anyRecord       `json:"children"`
	Codehash     libcommon.Hash    `json:"codehash"`
	From         libcommon.Address `json:"from"`
	Gas          uint64            `json:"gas"`
	GasUsed      uint64            `json:"gasUsed"`
	Input        hexutility.Bytes  `json:"input"`
	IsPrecompile bool              `json:"isPrecompile"`
	Output       hexutility.Bytes  `json:"output"`
	Status       int               `json:"status"`
	To           libcommon.Address `json:"to,omitempty" rlp:"optional"`
	Type         string            `json:"type"`
	Value        *uint256.Int      `json:"value"`
	Variant      string            `json:"variant"`
	create       bool
}

func (r *callRecord) finish(t *openchainTracer, output []byte, gasUsed uint64, err error) {
	r.GasUsed = gasUsed
	r.Output = common.CopyBytes(output)
	r.Status = 1
	if err != nil {
		r.Status = 0
	}
	if r.create {
		r.Codehash = t.env.IntraBlockState().GetCodeHash(r.To)
	}

	hmap, ok := t.addresses[r.To]
	if !ok {
		hmap = make(map[libcommon.Hash]*addressData)
		t.addresses[r.To] = hmap
	}
	if _, ok := hmap[r.Codehash]; !ok {
		hmap[r.Codehash] = &addressData{
			Errors:    make(map[string]interface{}),
			Events:    make(map[string]interface{}),
			Functions: make(map[string]interface{}),
			Label:     "",
		}
	}
}

type keccakState interface {
	hash.Hash
	Read([]byte) (int, error)
}

// not saving any data now
type addressData struct {
	Errors    map[string]interface{} `json:"errors"`
	Events    map[string]interface{} `json:"events"`
	Functions map[string]interface{} `json:"functions"`
	Label     string                 `json:"label"`
}

type openchainTracer struct {
	noopTracer
	callstack []*callRecord
	env       vm.VMInterface
	hasher    keccakState
	hasherBuf libcommon.Hash
	preimages map[libcommon.Hash]hexutility.Bytes
	addresses map[libcommon.Address]map[libcommon.Hash]*addressData
}

func newOpenchainTracer(ctx *tracers.Context, cfg json.RawMessage) (tracers.Tracer, error) {
	return &openchainTracer{
		callstack: []*callRecord{},
		hasher:    sha3.NewLegacyKeccak256().(keccakState),
		preimages: make(map[libcommon.Hash]hexutility.Bytes),
		addresses: make(map[libcommon.Address]map[libcommon.Hash]*addressData),
	}, nil
}

func (t *openchainTracer) pushRecord(r anyRecord) string {
	lst := t.callstack[len(t.callstack)-1]
	res := lst.Path + "." + strconv.Itoa(len(lst.Children))
	lst.Children = append(lst.Children, r)
	return res
}

func (t *openchainTracer) CaptureStart(env vm.VMInterface, from libcommon.Address, to libcommon.Address, precompile, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	t.callstack = append(t.callstack, &callRecord{
		Path:         "0",
		Children:     []anyRecord{},
		Codehash:     env.IntraBlockState().GetCodeHash(to),
		From:         from,
		Gas:          gas,
		Input:        common.CopyBytes(input),
		IsPrecompile: precompile,
		To:           to,
		Type:         "call",
		Value:        value,
		Variant:      "call",
		create:       create,
	})
	if create {
		t.callstack[0].Variant = "create"
	}
	t.env = env
}

func (t *openchainTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
	t.callstack[0].finish(t, output, gasUsed, err)
}

func (t *openchainTracer) CaptureState(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, rData []byte, depth int, err error) {
	switch op {
	case vm.SLOAD:
		slot := libcommon.Hash(scope.Stack.Peek().Bytes32())
		value := uint256.Int{}
		t.env.IntraBlockState().GetState(scope.Contract.Address(), &slot, &value)
		r := &sloadRecord{
			Slot:  slot,
			Type:  "sload",
			Value: libcommon.Hash(value.Bytes32()),
		}
		r.Path = t.pushRecord(r)
	case vm.SSTORE:
		slot := libcommon.Hash(scope.Stack.Peek().Bytes32())
		newValue := libcommon.Hash(scope.Stack.Back(1).Bytes32())
		oldValue := uint256.Int{}
		t.env.IntraBlockState().GetState(scope.Contract.Address(), &slot, &oldValue)
		r := &sstoreRecord{
			Slot:     slot,
			Type:     "sstore",
			OldValue: libcommon.Hash(oldValue.Bytes32()),
			NewValue: newValue,
		}
		r.Path = t.pushRecord(r)
	case vm.LOG0, vm.LOG1, vm.LOG2, vm.LOG3, vm.LOG4:
		size := int(op - vm.LOG0)
		stack := scope.Stack
		mStart := stack.Back(0)
		mSize := stack.Back(1)
		topics := make([]libcommon.Hash, size)
		for i := 0; i < size; i++ {
			topic := stack.Back(i + 2)
			topics[i] = libcommon.Hash(topic.Bytes32())
		}
		data := scope.Memory.GetCopy(int64(mStart.Uint64()), int64(mSize.Uint64()))
		r := &logRecord{
			Data:   data,
			Topics: topics,
			Type:   "log",
		}
		r.Path = t.pushRecord(r)
	case vm.KECCAK256:
		offset, size := scope.Stack.Peek(), scope.Stack.Back(1)
		data := scope.Memory.GetPtr(int64(offset.Uint64()), int64(size.Uint64()))
		t.hasher.Reset()
		t.hasher.Write(data)
		t.hasher.Read(t.hasherBuf[:])
		t.preimages[t.hasherBuf] = common.CopyBytes(data)
	case vm.CALL, vm.STATICCALL, vm.CALLCODE, vm.DELEGATECALL, vm.CREATE, vm.CREATE2:
		r := &callRecord{
			Children: []anyRecord{},
		}
		r.Path = t.pushRecord(r)
		t.callstack = append(t.callstack, r)
	}
}

func (t *openchainTracer) CaptureFault(pc uint64, op vm.OpCode, gas, cost uint64, _ *vm.ScopeContext, depth int, err error) {
}

func (t *openchainTracer) CaptureEnter(typ vm.OpCode, from libcommon.Address, to libcommon.Address, precompile, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	r := t.callstack[len(t.callstack)-1]
	r.Codehash = t.env.IntraBlockState().GetCodeHash(to)
	r.From = from
	r.Gas = gas
	r.Input = common.CopyBytes(input)
	r.IsPrecompile = precompile
	r.To = to
	r.Type = "call"
	if value != nil {
		r.Value = value
	} else {
		r.Value = uint256.NewInt(0)
	}
	r.Variant = strings.ToLower(typ.String())
	r.create = typ == vm.CREATE || typ == vm.CREATE2
}

func (t *openchainTracer) CaptureExit(output []byte, gasUsed uint64, err error) {
	t.callstack[len(t.callstack)-1].finish(t, output, gasUsed, err)
	t.callstack = t.callstack[:len(t.callstack)-1]
}

func (t *openchainTracer) GetResult() (json.RawMessage, error) {
	if len(t.callstack) != 1 {
		return nil, errors.New("incorrect number of top-level calls")
	}
	var st struct {
		Entrypoint *callRecord                                           `json:"entrypoint"`
		Preimages  map[libcommon.Hash]hexutility.Bytes                   `json:"preimages"`
		Addresses  map[libcommon.Address]map[libcommon.Hash]*addressData `json:"addresses"`
	}
	st.Entrypoint = t.callstack[0]
	st.Preimages = t.preimages
	st.Addresses = t.addresses
	res, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), nil
}

func (t *openchainTracer) Stop(err error) {
}
