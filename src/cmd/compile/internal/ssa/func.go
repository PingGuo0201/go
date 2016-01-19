// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssa

import "math"

// A Func represents a Go func declaration (or function literal) and
// its body.  This package compiles each Func independently.
type Func struct {
	Config     *Config     // architecture information
	Name       string      // e.g. bytes·Compare
	Type       Type        // type signature of the function.
	StaticData interface{} // associated static data, untouched by the ssa package
	Blocks     []*Block    // unordered set of all basic blocks (note: not indexable by ID)
	Entry      *Block      // the entry basic block
	bid        idAlloc     // block ID allocator
	vid        idAlloc     // value ID allocator

	scheduled bool // Values in Blocks are in final order

	// when register allocation is done, maps value ids to locations
	RegAlloc []Location

	// map from LocalSlot to set of Values that we want to store in that slot.
	NamedValues map[LocalSlot][]*Value
	// Names is a copy of NamedValues.Keys.  We keep a separate list
	// of keys to make iteration order deterministic.
	Names []LocalSlot

	freeValues *Value // free Values linked by argstorage[0].  All other fields except ID are 0/nil.
	freeBlocks *Block // free Blocks linked by succstorage[0].  All other fields except ID are 0/nil.
}

// NumBlocks returns an integer larger than the id of any Block in the Func.
func (f *Func) NumBlocks() int {
	return f.bid.num()
}

// NumValues returns an integer larger than the id of any Value in the Func.
func (f *Func) NumValues() int {
	return f.vid.num()
}

// newValue allocates a new Value with the given fields and places it at the end of b.Values.
func (f *Func) newValue(op Op, t Type, b *Block, line int32) *Value {
	var v *Value
	if f.freeValues != nil {
		v = f.freeValues
		f.freeValues = v.argstorage[0]
		v.argstorage[0] = nil
	} else {
		ID := f.vid.get()
		if int(ID) < len(f.Config.values) {
			v = &f.Config.values[ID]
		} else {
			v = &Value{ID: ID}
		}
	}
	v.Op = op
	v.Type = t
	v.Block = b
	v.Line = line
	b.Values = append(b.Values, v)
	return v
}

// freeValue frees a value.  It must no longer be referenced.
func (f *Func) freeValue(v *Value) {
	if v.Block == nil {
		f.Fatalf("trying to free an already freed value")
	}
	// Clear everything but ID (which we reuse).
	id := v.ID
	*v = Value{}
	v.ID = id
	v.argstorage[0] = f.freeValues
	f.freeValues = v
}

// newBlock allocates a new Block of the given kind and places it at the end of f.Blocks.
func (f *Func) NewBlock(kind BlockKind) *Block {
	var b *Block
	if f.freeBlocks != nil {
		b = f.freeBlocks
		f.freeBlocks = b.succstorage[0]
		b.succstorage[0] = nil
	} else {
		ID := f.bid.get()
		if int(ID) < len(f.Config.blocks) {
			b = &f.Config.blocks[ID]
		} else {
			b = &Block{ID: ID}
		}
	}
	b.Kind = kind
	b.Func = f
	b.Preds = b.predstorage[:0]
	b.Succs = b.succstorage[:0]
	b.Values = b.valstorage[:0]
	f.Blocks = append(f.Blocks, b)
	return b
}

func (f *Func) freeBlock(b *Block) {
	if b.Func == nil {
		f.Fatalf("trying to free an already freed block")
	}
	// Clear everything but ID (which we reuse).
	id := b.ID
	*b = Block{}
	b.ID = id
	b.succstorage[0] = f.freeBlocks
	f.freeBlocks = b
}

// NewValue0 returns a new value in the block with no arguments and zero aux values.
func (b *Block) NewValue0(line int32, op Op, t Type) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Args = v.argstorage[:0]
	return v
}

// NewValue returns a new value in the block with no arguments and an auxint value.
func (b *Block) NewValue0I(line int32, op Op, t Type, auxint int64) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Args = v.argstorage[:0]
	return v
}

// NewValue returns a new value in the block with no arguments and an aux value.
func (b *Block) NewValue0A(line int32, op Op, t Type, aux interface{}) *Value {
	if _, ok := aux.(int64); ok {
		// Disallow int64 aux values.  They should be in the auxint field instead.
		// Maybe we want to allow this at some point, but for now we disallow it
		// to prevent errors like using NewValue1A instead of NewValue1I.
		b.Fatalf("aux field has int64 type op=%s type=%s aux=%v", op, t, aux)
	}
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Aux = aux
	v.Args = v.argstorage[:0]
	return v
}

// NewValue returns a new value in the block with no arguments and both an auxint and aux values.
func (b *Block) NewValue0IA(line int32, op Op, t Type, auxint int64, aux interface{}) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Aux = aux
	v.Args = v.argstorage[:0]
	return v
}

// NewValue1 returns a new value in the block with one argument and zero aux values.
func (b *Block) NewValue1(line int32, op Op, t Type, arg *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Args = v.argstorage[:1]
	v.argstorage[0] = arg
	return v
}

// NewValue1I returns a new value in the block with one argument and an auxint value.
func (b *Block) NewValue1I(line int32, op Op, t Type, auxint int64, arg *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Args = v.argstorage[:1]
	v.argstorage[0] = arg
	return v
}

// NewValue1A returns a new value in the block with one argument and an aux value.
func (b *Block) NewValue1A(line int32, op Op, t Type, aux interface{}, arg *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Aux = aux
	v.Args = v.argstorage[:1]
	v.argstorage[0] = arg
	return v
}

// NewValue1IA returns a new value in the block with one argument and both an auxint and aux values.
func (b *Block) NewValue1IA(line int32, op Op, t Type, auxint int64, aux interface{}, arg *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Aux = aux
	v.Args = v.argstorage[:1]
	v.argstorage[0] = arg
	return v
}

// NewValue2 returns a new value in the block with two arguments and zero aux values.
func (b *Block) NewValue2(line int32, op Op, t Type, arg0, arg1 *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Args = v.argstorage[:2]
	v.argstorage[0] = arg0
	v.argstorage[1] = arg1
	return v
}

// NewValue2I returns a new value in the block with two arguments and an auxint value.
func (b *Block) NewValue2I(line int32, op Op, t Type, auxint int64, arg0, arg1 *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Args = v.argstorage[:2]
	v.argstorage[0] = arg0
	v.argstorage[1] = arg1
	return v
}

// NewValue3 returns a new value in the block with three arguments and zero aux values.
func (b *Block) NewValue3(line int32, op Op, t Type, arg0, arg1, arg2 *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = 0
	v.Args = []*Value{arg0, arg1, arg2}
	return v
}

// NewValue3I returns a new value in the block with three arguments and an auxint value.
func (b *Block) NewValue3I(line int32, op Op, t Type, auxint int64, arg0, arg1, arg2 *Value) *Value {
	v := b.Func.newValue(op, t, b, line)
	v.AuxInt = auxint
	v.Args = []*Value{arg0, arg1, arg2}
	return v
}

// ConstInt returns an int constant representing its argument.
func (f *Func) ConstBool(line int32, t Type, c bool) *Value {
	// TODO: cache?
	i := int64(0)
	if c {
		i = 1
	}
	return f.Entry.NewValue0I(line, OpConstBool, t, i)
}
func (f *Func) ConstInt8(line int32, t Type, c int8) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst8, t, int64(c))
}
func (f *Func) ConstInt16(line int32, t Type, c int16) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst16, t, int64(c))
}
func (f *Func) ConstInt32(line int32, t Type, c int32) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst32, t, int64(c))
}
func (f *Func) ConstInt64(line int32, t Type, c int64) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst64, t, c)
}
func (f *Func) ConstFloat32(line int32, t Type, c float64) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst32F, t, int64(math.Float64bits(c)))
}
func (f *Func) ConstFloat64(line int32, t Type, c float64) *Value {
	// TODO: cache?
	return f.Entry.NewValue0I(line, OpConst64F, t, int64(math.Float64bits(c)))
}

func (f *Func) Logf(msg string, args ...interface{})   { f.Config.Logf(msg, args...) }
func (f *Func) Fatalf(msg string, args ...interface{}) { f.Config.Fatalf(f.Entry.Line, msg, args...) }
func (f *Func) Unimplementedf(msg string, args ...interface{}) {
	f.Config.Unimplementedf(f.Entry.Line, msg, args...)
}

func (f *Func) Free() {
	// Clear values.
	n := f.vid.num()
	if n > len(f.Config.values) {
		n = len(f.Config.values)
	}
	for i := 1; i < n; i++ {
		f.Config.values[i] = Value{}
		f.Config.values[i].ID = ID(i)
	}

	// Clear blocks.
	n = f.bid.num()
	if n > len(f.Config.blocks) {
		n = len(f.Config.blocks)
	}
	for i := 1; i < n; i++ {
		f.Config.blocks[i] = Block{}
		f.Config.blocks[i].ID = ID(i)
	}

	// Unregister from config.
	if f.Config.curFunc != f {
		f.Fatalf("free of function which isn't the last one allocated")
	}
	f.Config.curFunc = nil
	*f = Func{} // just in case
}