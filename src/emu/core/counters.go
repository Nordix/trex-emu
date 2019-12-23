package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"unsafe"
)

/* CCounter Type */
const ScINFO = 0x12
const ScWARNING = 0x13
const ScERROR = 0x14

type cCounterVal struct {
	Counter interface{} `json:"cnt"`
}

type CCounterRec struct {
	Counter  interface{} `json:"-"`
	Name     string      `json:"name"`
	Help     string      `json:"help"`
	Unit     string      `json:"unit"`
	DumpZero bool        `json:"zero"`
	Info     uint8       `json:"info"` // see scINFO,scWARNING,scERROR
}

func (o *CCounterRec) IsValid() bool {
	if (o.DumpZero) || (o.IsZero() == false) {
		return true
	} else {
		return false
	}
}

func (o *CCounterRec) MarshalValue() []byte {
	var res []byte
	a := &cCounterVal{Counter: o.Counter}
	res, _ = json.Marshal(a)
	return res
}

func (o *CCounterRec) MarshalMetaAndVal() []byte {
	res, _ := json.Marshal(o)
	return res
}

func (o *CCounterRec) IsZero() bool {
	zero := false
	switch o.Counter.(type) {
	case *uint32:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		a := (*uint32)(unsafe.Pointer(elm.Addr().Pointer()))
		if *a == 0 {
			zero = true
		}
	case *uint64:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		a := (*uint64)(unsafe.Pointer(elm.Addr().Pointer()))
		if *a == 0 {
			zero = true
		}
	case *float32:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		a := (*float32)(unsafe.Pointer(elm.Addr().Pointer()))
		if *a == 0.0 {
			zero = true
		}
	case *float64:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		a := (*float64)(unsafe.Pointer(elm.Addr().Pointer()))
		if *a == 0.0 {
			zero = true
		}
	default:

	}
	return zero
}

func (o *CCounterRec) GetValAsString() string {
	s := ""
	switch o.Counter.(type) {
	case *uint32:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		s = fmt.Sprintf("%v", elm)
	case *uint64:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		s = fmt.Sprintf("%v", elm)
	case *float32:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		s = fmt.Sprintf("%v", elm)
	case *float64:
		val := reflect.ValueOf(o.Counter)
		elm := val.Elem()
		s = fmt.Sprintf("%v", elm)
	default:
		s = "N/A"
	}
	return s
}

func (o *CCounterRec) Dump() {
	if !o.IsZero() {
		s := o.GetValAsString()
		fmt.Printf("%-30s : %10s \n", o.Name, s)
	}
}

type CCounterDb struct {
	Name string         `json:"name"`
	Vec  []*CCounterRec `json:"meta"`
}

func NewCCounterDb(name string) *CCounterDb {
	return &CCounterDb{Name: name, Vec: []*CCounterRec{}}
}

func (o *CCounterDb) Add(cnt *CCounterRec) {
	o.Vec = append(o.Vec, cnt)
}

func (o *CCounterDb) Dump() {
	fmt.Println(" counters " + o.Name + " db")
	for _, obj := range o.Vec {
		obj.Dump()
	}
	fmt.Println(" ===")
}

func (o *CCounterDb) MarshalValues(zero bool) map[string]interface{} {
	m := make(map[string]interface{})
	for _, obj := range o.Vec {
		if zero || obj.IsValid() {
			m[obj.Name] = obj.Counter
		}
	}
	return (m)
}

func (o *CCounterDb) MarshalMeta() []byte {
	res, _ := json.Marshal(o)
	return (res)
}

type CCounterDbVec struct {
	Name string        `json:"name"`
	Vec  []*CCounterDb `json:"vec"`
}

func NewCCounterDbVec(name string) *CCounterDbVec {
	return &CCounterDbVec{Name: name, Vec: []*CCounterDb{}}
}

func (o *CCounterDbVec) Add(cnt *CCounterDb) {
	o.Vec = append(o.Vec, cnt)
}

func (o *CCounterDbVec) AddVec(cnt *CCounterDbVec) {
	for _, vec := range cnt.Vec {
		o.Add(vec)
	}
}

func (o *CCounterDbVec) Dump() {
	fmt.Println(" counters " + o.Name + " dbvec")
	for _, obj := range o.Vec {
		obj.Dump()
	}
	fmt.Println(" ===")
}

func (o *CCounterDbVec) MarshalValues(zero bool) map[string]interface{} {
	m := make(map[string]interface{})
	for _, obj := range o.Vec {
		m[obj.Name] = obj.MarshalValues(zero)
	}
	return (m)
}

func (o *CCounterDbVec) MarshalMeta() map[string]interface{} {
	m := make(map[string]interface{})
	for _, obj := range o.Vec {
		m[obj.Name] = obj
	}
	return (m)
}
