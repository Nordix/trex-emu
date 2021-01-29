// Copyright (c) 2020 Cisco Systems and/or its affiliates.
// Licensed under the Apache License, Version 2.0 (the "License");
// that can be found in the LICENSE file in the root of the source
// tree.

package core

import (
	"encoding/json"
	"io"
	"os"
)

type VethStats struct {
	TxPkts           uint64
	TxBytes          uint64
	RxPkts           uint64
	RxBytes          uint64
	RxParseErr       uint64
	RxZmqErr         uint64
	RxBatch          uint64
	TxBatch          uint64
	TxDropNotResolve uint64 /* no resolved dg */

}

func NewVethStatsDb(o *VethStats) *CCounterDb {
	db := NewCCounterDb("veth")

	db.Add(&CCounterRec{
		Counter:  &o.RxZmqErr,
		Name:     "RxZmqErr",
		Help:     "RxZmqErr",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScERROR})

	db.Add(&CCounterRec{
		Counter:  &o.TxPkts,
		Name:     "TxPkts",
		Help:     "TxPkts",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.TxBytes,
		Name:     "TxBytes",
		Help:     "TxBytes",
		Unit:     "bytes",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.RxPkts,
		Name:     "RxPkts",
		Help:     "RxPkts",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.RxBytes,
		Name:     "RxBytes",
		Help:     "RxBytes",
		Unit:     "bytes",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.TxDropNotResolve,
		Name:     "TxDropNotResolve",
		Help:     "TxDropNotResolve",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScERROR})

	db.Add(&CCounterRec{
		Counter:  &o.RxParseErr,
		Name:     "RxParseErr",
		Help:     "RxParseErr",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScERROR})

	db.Add(&CCounterRec{
		Counter:  &o.RxBatch,
		Name:     "RxBatch",
		Help:     "RxBatch",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScERROR})

	db.Add(&CCounterRec{
		Counter:  &o.TxBatch,
		Name:     "TxBatch",
		Help:     "TxBatch",
		Unit:     "pkts",
		DumpZero: false,
		Info:     ScERROR})

	return db
}

/*VethIF represent a way to send and receive packet */
type VethIF interface {

	/* Flush the tx buffer and send the packets */
	FlushTx()

	/* the mbuf should be ready for sending*/
	Send(m *Mbuf)

	// SendBuffer get a buffer as input, should allocate mbuf and call send
	SendBuffer(unicast bool, c *CClient, b []byte)

	// get
	OnRx(m *Mbuf)

	/* get the veth stats */
	GetStats() *VethStats

	SimulatorCheckRxQueue()

	SimulatorCleanup()

	SetDebug(monitor bool, monitorFile *os.File, capture bool)

	GetCdb() *CCounterDb

	AppendSimuationRPC(request []byte)

	GetC() chan []byte

	StartRxThread()

	OnRxStream(msg []byte)
}

type VethIFSim interface {

	/* Simulate a DUT that gets a mbuf and response with mbuf if needed or nil if there is no need to response */
	ProcessTxToRx(m *Mbuf) *Mbuf
}

type VethIFSimulator struct {
	vec         []*Mbuf
	rxvec       []*Mbuf
	rpcQue      [][]byte
	stats       VethStats
	tctx        *CThreadCtx
	Sim         VethIFSim // interface per test to simulate DUT
	Record      bool      // to a buffer
	K12Monitor  bool      // K12 packet monitoring to monitorDest
	monitorFile *os.File  // File to print the K12 packet captured. Default is stdout.
	cdb         *CCounterDb
}

func (o *VethIFSimulator) Create(ctx *CThreadCtx) {
	o.vec = make([]*Mbuf, 0)
	o.rxvec = make([]*Mbuf, 0)
	o.rpcQue = make([][]byte, 0)
	o.tctx = ctx
	o.cdb = NewVethStatsDb(&o.stats)
}

func (o *VethIFSimulator) FlushTx() {
	if len(o.vec) == 0 {
		return
	}
	o.stats.TxBatch++
	for _, m := range o.vec {
		if !m.IsContiguous() {
			panic(" mbuf should be contiguous  ")
		}
		if o.K12Monitor {
			io.WriteString(o.monitorFile, "\n ->TX<- \n")
			m.DumpK12(o.tctx.GetTickSimInSec(), o.monitorFile)
		}
		if o.Record {
			o.tctx.SimRecordAppend(m.GetRecord(o.tctx.GetTickSimInSec(), "tx"))
		}
		m.FreeMbuf()
	}
	o.vec = o.vec[:0]

}

func (o *VethIFSimulator) AppendSimuationRPC(request []byte) {
	o.rpcQue = append(o.rpcQue, request)
}

func (o *VethIFSimulator) Send(m *Mbuf) {

	o.stats.TxPkts++
	o.stats.TxBytes += uint64(m.PktLen())
	if !m.IsContiguous() {
		m1 := m.GetContiguous(&o.tctx.MPool)
		m.FreeMbuf()
		o.vec = append(o.vec, m1)
	} else {
		o.vec = append(o.vec, m)
	}
}

// SendBuffer get a buffer as input, should allocate mbuf and call send
func (o *VethIFSimulator) SendBuffer(unicast bool, c *CClient, b []byte) {
	var vport uint16
	vport = c.Ns.GetVport()
	m := o.tctx.MPool.Alloc(uint16(len(b)))
	m.SetVPort(vport)
	m.Append(b)
	if unicast {
		if c.DGW == nil {
			m.FreeMbuf()
			o.stats.TxDropNotResolve++
			return
		}
		if !c.DGW.IpdgResolved {
			m.FreeMbuf()
			o.stats.TxDropNotResolve++
			return
		}
		p := m.GetData()
		copy(p[6:12], c.Mac[:])
		copy(p[0:6], c.DGW.IpdgMac[:])
	}
	o.Send(m)
}

// get the packet
func (o *VethIFSimulator) OnRx(m *Mbuf) {
	o.stats.RxPkts++
	o.stats.RxBytes += uint64(m.PktLen())
	if o.K12Monitor {
		io.WriteString(o.monitorFile, "\n ->RX<- \n")
		m.DumpK12(o.tctx.GetTickSimInSec(), o.monitorFile)
	}
	if o.Record {
		o.tctx.SimRecordAppend(m.GetRecord(o.tctx.GetTickSimInSec(), "rx"))
	}

	o.tctx.HandleRxPacket(m)
}

/* get the veth stats */
func (o *VethIFSimulator) GetStats() *VethStats {
	return &o.stats
}

func jsonInterface(d []byte) interface{} {

	var v interface{}
	err := json.Unmarshal(d, &v)
	if err != nil {
		return err
	}
	return v
}

func (o *VethIFSimulator) handleRpcQueue() {

	for _, req := range o.rpcQue {
		if o.Record {
			obj := make(map[string]interface{}, 0)
			obj["rpc-req"] = jsonInterface(req)
			o.tctx.SimRecordAppend(obj)

		}
		res := o.tctx.rpc.HandleReq(req)
		if o.Record {
			objres := make(map[string]interface{}, 0)
			objres["rpc-res"] = jsonInterface(res)
			o.tctx.SimRecordAppend(objres)
		}
	}
	o.rpcQue = o.rpcQue[:0]
}

func (o *VethIFSimulator) SimulatorCheckRxQueue() {

	o.handleRpcQueue()
	for _, m := range o.vec {
		if o.K12Monitor {
			m.DumpK12(o.tctx.GetTickSimInSec(), o.monitorFile)
		}
		if o.Record {
			o.tctx.SimRecordAppend(m.GetRecord(o.tctx.GetTickSimInSec(), "tx"))
		}

		mrx := o.Sim.ProcessTxToRx(m)

		if mrx != nil {
			o.rxvec = append(o.rxvec, mrx)
		}
	}
	o.vec = o.vec[:0]

	for _, m := range o.rxvec {
		o.OnRx(m)
	}
	o.rxvec = o.rxvec[:0]
}

func (o *VethIFSimulator) SimulatorCleanup() {

	for _, m := range o.vec {
		m.FreeMbuf()
	}
	o.vec = nil
	o.rxvec = nil
}

func (o *VethIFSimulator) SetDebug(monitor bool, monitorFile *os.File, capture bool) {
	o.K12Monitor = monitor
	o.Record = capture
	o.monitorFile = monitorFile
}

func (o *VethIFSimulator) GetC() chan []byte {
	return nil
}

func (o *VethIFSimulator) StartRxThread() {
	panic(" StartRxThread() should not be used in VethIFSimulator ")
}

func (o *VethIFSimulator) OnRxStream(stream []byte) {
	panic(" OnRxStream() should not be used in VethIFSimulator ")
}

func (o *VethIFSimulator) GetCdb() *CCounterDb {
	return o.cdb
}

type VethSink struct{}

func (o *VethSink) ProcessTxToRx(m *Mbuf) *Mbuf {
	m.FreeMbuf()
	return nil
}

func (o *VethSink) Send(m *Mbuf) {
	m.FreeMbuf()
}
