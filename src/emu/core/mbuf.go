// Copyright (c) 2020 Cisco Systems and/or its affiliates.
// Licensed under the Apache License, Version 2.0 (the "License");
// that can be found in the LICENSE file in the root of the source
// tree.

package core

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"unsafe"
)

/*mbuf

A simplified version of DPDK/BSD mbuf library

see
https://doc.dpdk.org/guides/prog_guide/mbuf_lib.html

1. It uses pool of memory for each packet size and cache the mbuf
2. The performance is about ~x20 relative to simple allocation (~20nsec vs 900nsec)
3. The memory is normal allocated memory from heap/GC
3. It does not support attach/detach for multicast  (simplification)
4. Single threaded -- each thread should have its own local pool


	var pool MbufPoll
	pool.Init(1024) 			# cache up to 1024 active packets per pool for -> ~9MB
	pool.DumpStats()			# dump statistic
	m := pool.Alloc(128)		#
	m.Dump()					# Dump the mbuf
	m.Append([]byte{1,2,3})     #append data
	m.FreeMbuf()   				#free the memory to cache. It is not mandatory to free it

*/

const lRTE_PKTMBUF_HEADROOM = 64
const lMBUF_INVALID_PORT = 0xffff
const lIND_ATTACHED_MBUF = 0x2

//MAX_PACKET_SIZE the maximum packet size
const MAX_PACKET_SIZE uint16 = 9 * 1024
const MBUF_RX_POOL_SIZE = 2048 // pool used by rx size

// MbufPoll cache of mbufs per packet size
type MbufPoll struct {
	pools []MbufPollSize
	stats MbufPollStats
	Cdbv  *CCounterDbVec
	Cdb   *CCounterDb
}

var poolSizes = [...]uint16{128, 256, 512, 1024, MBUF_RX_POOL_SIZE, 4096, MAX_PACKET_SIZE}

//GetMaxPacketSize return the maximum
func (o *MbufPoll) GetMaxPacketSize() uint16 {
	return (MAX_PACKET_SIZE)
}

// Init init all the pools (per size).
// maxCacheSize - how many packets to cache
func (o *MbufPoll) Init(maxCacheSize uint32) {

	o.pools = make([]MbufPollSize, len(poolSizes))
	o.Cdbv = NewCCounterDbVec("mbuf_pool")
	for i, s := range poolSizes {
		o.pools[i].Init(maxCacheSize, s)
		o.Cdbv.Add(o.pools[i].cdb)
	}
	o.Cdb = NewMbufStatsDb(&o.stats)
	o.Cdb.Name = fmt.Sprintf("mbuf-pool")
	o.Cdb.IOpt = o
}

func (o *MbufPoll) PreUpdate() {
	o.stats.Clear()
	for i := range poolSizes {
		o.stats.Add(&o.pools[i].stats)
	}
}

func (o *MbufPoll) ClearCache() {
	for i, _ := range poolSizes {
		if o.pools[i].stats.InUsed() > 0 {
			s := fmt.Sprintf(" mbuf leakage pool index %d", i)
			panic(s)
		}
		o.pools[i].ClearCache()
	}
}

func (o *MbufPoll) GetPoolBySize(size uint16) *MbufPollSize {
	for i, ps := range poolSizes {
		if size <= ps {
			return &o.pools[i]
		}
	}
	s := fmt.Sprintf(" MbufPoll.Alloc size is too big %d ", size)
	panic(s)
}

// Alloc new mbuf from the right pool
func (o *MbufPoll) Alloc(size uint16) *Mbuf {
	for i, ps := range poolSizes {
		if size <= ps {
			return o.pools[i].NewMbuf()
		}
	}
	s := fmt.Sprintf(" MbufPoll.Alloc size is too big %d ", size)
	panic(s)
}
func (o *MbufPoll) GetCdb() *CCounterDb {
	_ = o.GetStats()
	return o.Cdb
}

// GetStats return accumulated statistics for all pools
func (o *MbufPoll) GetStats() *MbufPollStats {
	o.PreUpdate()
	return &o.stats
}

// DumpStats dump statistics
func (o *MbufPoll) DumpStats() {
	fmt.Println(" size  | stats ")
	fmt.Println(" ----------------------")
	for i, s := range poolSizes {
		p := &o.pools[i].stats
		fmt.Printf(" %-04d  | %3.0f%% (inused:%v)  %+v   \n", s, p.HitRate(), p.InUsed(), *p)
	}

}

// MbufPollStats per pool statistic
type MbufPollStats struct {
	CntAlloc      uint64
	CntFree       uint64
	CntCacheAlloc uint64
	CntCacheFree  uint64
}

func NewMbufStatsDb(o *MbufPollStats) *CCounterDb {
	db := NewCCounterDb("mbuf")
	db.Add(&CCounterRec{
		Counter:  &o.CntAlloc,
		Name:     "mbufAlloc",
		Help:     "allocation of mbufs",
		Unit:     "ops",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.CntFree,
		Name:     "mbufFree",
		Help:     "deallocation of mbufs",
		Unit:     "ops",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.CntCacheAlloc,
		Name:     "mbufAllocCache",
		Help:     "allocation of mbufs from cache",
		Unit:     "ops",
		DumpZero: false,
		Info:     ScINFO})

	db.Add(&CCounterRec{
		Counter:  &o.CntCacheFree,
		Name:     "mbufFreeCache",
		Help:     "deallocation of mbufs from cache",
		Unit:     "ops",
		DumpZero: false,
		Info:     ScINFO})

	return db
}

func (o *MbufPollStats) Clear() {
	o.CntAlloc = 0
	o.CntFree = 0
	o.CntCacheAlloc = 0
	o.CntCacheFree = 0
}

func (o *MbufPollStats) InUsed() uint64 {
	return o.CntAlloc + o.CntCacheAlloc - (o.CntCacheFree + o.CntFree)
}

//HitRate return the hit rate in precent
func (o *MbufPollStats) HitRate() float32 {
	if o.CntCacheFree == 0 {
		return 0.0
	}
	return float32(o.CntCacheAlloc) * 100.0 / float32(o.CntCacheAlloc+o.CntAlloc)
}

// Add o = o + obj
func (o *MbufPollStats) Add(obj *MbufPollStats) {
	o.CntAlloc += obj.CntAlloc
	o.CntFree += obj.CntFree
	o.CntCacheAlloc += obj.CntCacheAlloc
	o.CntCacheFree += obj.CntCacheFree
}

// MbufPollSize pool per size
type MbufPollSize struct {
	mlist        DList
	cacheSize    uint32 /*	the active cache size */
	maxCacheSize uint32 /*	the maximum cache size */
	mbufSize     uint16 /* buffer size without the lRTE_PKTMBUF_HEADROOM */
	stats        MbufPollStats
	cdb          *CCounterDb
}

// Init the pool
func (o *MbufPollSize) Init(maxCacheSize uint32, mbufSize uint16) {
	o.mlist.SetSelf()
	o.maxCacheSize = maxCacheSize
	o.mbufSize = mbufSize
	o.cdb = NewMbufStatsDb(&o.stats)
	o.cdb.Name = fmt.Sprintf("mbuf-%d", mbufSize)
}

func (o *MbufPollSize) getHead() *Mbuf {
	h := o.mlist.RemoveLast()
	o.cacheSize -= 1
	return (toMbuf(h))
}

func (o *MbufPollSize) ClearCache() {

	for {
		if o.cacheSize > 0 {
			_ = o.getHead()
		} else {
			break
		}
	}

}

// NewMbuf alloc new mbuf with the right size
func (o *MbufPollSize) NewMbuf() *Mbuf {

	// ignore the size of the packet
	var m *Mbuf
	if o.cacheSize > 0 {
		o.stats.CntCacheAlloc++
		m = o.getHead()
		m.resetMbuf()
		return (m)
	}

	// allocate new mbuf
	m = new(Mbuf)
	m.bufLen = uint16(o.mbufSize) + lRTE_PKTMBUF_HEADROOM
	m.data = make([]byte, m.bufLen)
	m.pool = o
	m.resetMbuf()
	o.stats.CntAlloc++
	return (m)
}

// FreeMbuf free mbuf to cache
func (o *MbufPollSize) FreeMbuf(obj *Mbuf) {

	if o.cacheSize < o.maxCacheSize {
		o.mlist.AddLast(&obj.dlist)
		o.cacheSize++
		o.stats.CntCacheFree++
	} else {
		o.stats.CntFree++
	}
}

func toMbuf(dlist *DList) *Mbuf {
	return (*Mbuf)(unsafe.Pointer(dlist))
}

type MbufJson struct {
	Time      float64 `json:"time"`
	Meta      string  `json:"meta"`
	K12PktLen uint32  `json:"len"`
	K12data   string  `json:"data"`
}

// Mbuf represent a chunk of packet
type Mbuf struct {
	dlist     DList // point to the next mbuf
	pool      *MbufPollSize
	pktLen    uint32
	refcnt    uint16
	olFlags   uint16
	dataLen   uint16
	dataOff   uint16
	bufLen    uint16
	nbSegs    uint16
	port      uint16
	timestamp uint64
	data      []byte
}

func (o *Mbuf) DeepClone() *Mbuf {
	m := o.pool.NewMbuf()
	m.SetVPort(o.port)
	m.Append(o.GetData())
	return m
}

func (o *Mbuf) getRefCnt() uint16 {
	return o.refcnt
}

func (o *Mbuf) setRefCnt(n uint16) {
	o.refcnt = n
}

func (o *Mbuf) updateRefCnt(update int16) {
	o.refcnt = o.refcnt + uint16(update)
}

func (o *Mbuf) resetMbuf() {

	o.dlist.SetSelf()
	o.dataLen = 0
	o.pktLen = 0
	o.nbSegs = 1
	o.dataOff = lRTE_PKTMBUF_HEADROOM
	o.port = lMBUF_INVALID_PORT
	o.olFlags = 0
	o.refcnt = 1
	o.timestamp = 0
}

func (o *Mbuf) isDirect() bool {
	if o.olFlags&(lIND_ATTACHED_MBUF) == 0 {
		return true
	} else {
		return false
	}
}

func (o *Mbuf) VPort() uint16 {
	return o.port
}

func (o *Mbuf) SetVPort(vport uint16) {
	o.port = vport
}

// PktLen return the packet len. Valid only for the header mbuf
func (o *Mbuf) PktLen() uint32 {
	return o.pktLen
}

// DataLen return the amount of data valid for this mbuf
func (o *Mbuf) DataLen() uint16 {
	return o.dataLen
}

// LastSeg return the last mbuf
func (o *Mbuf) LastSeg() *Mbuf {
	return toMbuf(o.dlist.Prev())
}

// Tailroom return the amount of bytes left in the tail - per mbuf
func (o *Mbuf) Tailroom() uint16 {
	return (o.bufLen - o.dataOff - o.dataLen)
}

// Headroom return the amount of bytes left in the head - per mbuf
func (o *Mbuf) Headroom() uint16 {
	return o.dataOff
}

// Prepend - prepend buffer. panic in case there is no enough room. check before with Headroom()
func (o *Mbuf) Prepend(d []byte) {
	var size uint16
	size = uint16(len(d))
	if size > o.dataOff {
		s := fmt.Sprintf(" prepend %d bytes to mbuf remain size %d", size, o.dataOff)
		panic(s)
	}
	o.dataOff -= size
	o.dataLen += size
	o.pktLen += uint32(size)
	copy(o.data[o.dataOff:], d)
}

//GetData return the byte stream of current object
func (o *Mbuf) GetData() []byte {
	return o.data[o.dataOff:(o.dataOff + o.dataLen)]
}

//Append  Append buffer to an mbuf - panic in case there is no room. check left space with Tailroom()
func (o *Mbuf) Append(d []byte) {
	last := o.LastSeg()
	off := last.dataOff + last.dataLen
	n := last.bufLen - off
	var size uint16
	size = uint16(len(d))
	if size > n {
		s := fmt.Sprintf(" append %d to mbuf remain size %d", size, n)
		panic(s)
	}

	copy(last.data[off:], d)
	o.pktLen += uint32(size)
	last.dataLen += size
}

func (o *Mbuf) AppendBytes(bytes uint16) {
	last := o.LastSeg()
	off := last.dataOff + last.dataLen
	n := last.bufLen - off
	var size uint16
	size = bytes
	if size > n {
		s := fmt.Sprintf(" append %d to mbuf remain size %d", size, n)
		panic(s)
	}
	o.pktLen += uint32(size)
	last.dataLen += size
}

// Trim - Remove len bytes of data at the end of the mbuf.
func (o *Mbuf) Trim(dlen uint16) {
	last := o.LastSeg()
	if dlen > last.dataLen {
		s := fmt.Sprintf(" trim %d bigger than packet len %d", dlen, last.dataLen)
		panic(s)
	}
	last.dataLen -= dlen
	o.pktLen -= uint32(dlen)
}

// IsContiguous - valid for header mbuf, return  true in case it has only one mbuf in chain
func (o *Mbuf) IsContiguous() bool {
	if o.nbSegs == 1 {
		return true
	} else {
		return false
	}
}

// Adj - Remove len bytes at the beginning of an mbuf.
func (o *Mbuf) Adj(dlen uint16) int {

	if dlen > o.dataLen {
		return -1
	}
	o.dataLen -= dlen
	o.dataOff += dlen
	o.pktLen -= uint32(dlen)
	return 0
}

// AppendMbuf add mbuf to be last in chain
func (o *Mbuf) AppendMbuf(m *Mbuf) {
	o.dlist.AddLast(&m.dlist)
	o.pktLen += uint32(m.dataLen)
	o.nbSegs++
}

// DetachLast remove the first mbuf and return it
func (o *Mbuf) DetachLast() *Mbuf {
	m := toMbuf(o.dlist.RemoveLast())
	o.pktLen -= uint32(m.dataLen)
	o.nbSegs--
	return m
}

// DetachFirst remove the last mbuf and return it
func (o *Mbuf) DetachFirst() *Mbuf {
	m := toMbuf(o.dlist.RemoveFirst())
	o.pktLen -= uint32(m.dataLen)
	o.nbSegs--
	return m
}

// Next return next mbuf. should be compared to head node
func (o *Mbuf) Next() *Mbuf {
	return toMbuf(o.dlist.Next())
}

func (o *Mbuf) freeMbufSeg() {
	if o.refcnt != 1 {
		s := fmt.Sprintf(" refcnt should be 1")
		panic(s)
	}
	// give the mbuf back
	o.pool.FreeMbuf(o)
}

//FreeMbuf to original pool, the mbuf can't be used after this function
func (o *Mbuf) FreeMbuf() {

	var next *Mbuf
	m := o
	for {
		next = m.Next()
		m.freeMbufSeg()
		if next == o {
			break
		}
		m = next
	}
	o = nil
}

// SanityCheck verify that mbuf is OK, panic if not
func (o *Mbuf) SanityCheck(header bool) {
	if o.pool == nil {
		panic(" pool is nil ")
	}

	if o.refcnt != 1 {
		panic(" refcnt is not supported ")
	}

	if !header {
		return
	}

	if uint32(o.dataLen) > o.pktLen {
		panic(" bad data_len ")
	}
	segs := o.nbSegs
	pktLen := o.pktLen
	m := o

	for {
		if m.dataOff > m.bufLen {
			panic(" data offset too big in mbuf segment ")
		}
		if m.dataOff+m.dataLen > m.bufLen {
			panic(" data length too big in mbuf segment ")
		}
		segs -= 1
		pktLen -= uint32(m.dataLen)
		m = m.Next()
		if m == o {
			break
		}
	}
	if segs > 0 {
		panic(" bad nb_segs ")
	}
	if pktLen > 0 {
		panic(" bad pkt_len")
	}
}

// Dump debug dump as buffer
func (o *Mbuf) String() string {
	var next *Mbuf
	first := true
	cnt := 0
	s := ""
	m := o
	for {
		next = m.Next()
		s += fmt.Sprintf(" %d: ", cnt)
		if first {
			s += fmt.Sprintf(" pktlen : %d, ", m.pktLen)
			s += fmt.Sprintf(" segs   : %d, ", m.nbSegs)
			s += fmt.Sprintf(" ports  : %d, ", m.port)
		}
		s += fmt.Sprintf(" buflen  : %d ", m.bufLen)
		s += fmt.Sprintf(" dataLen : %d ", m.dataLen)
		if m.dataLen > 0 {
			s += fmt.Sprintf("\n%s\n", hex.Dump(m.GetData()))
		} else {
			s += fmt.Sprintf("\n Empty\n")
		}
		if next == o {
			break
		}
		first = false
		m = next
		cnt++
	}
	return s
}

// Dump - dump
func (o *Mbuf) Dump() {
	fmt.Println(o)
}

func (o *Mbuf) GetContiguous(pool *MbufPoll) *Mbuf {

	if o.IsContiguous() {
		panic(" this mbuf is already Contiguous ")
	}
	var next *Mbuf
	m := o
	tom := pool.Alloc(uint16(o.PktLen()))
	for {
		next = m.Next()
		tom.Append(m.GetData()[:])
		if next == o {
			break
		}
		m = next
	}

	return tom
}

func reminder(a float64) float64 {
	return (a - float64(uint64(a)))
}

func (o *Mbuf) GetK12String() string {
	var res string

	var next *Mbuf
	m := o
	for {
		next = m.Next()
		if m.dataLen > 0 {
			for _, c := range m.GetData() {
				res += fmt.Sprintf("%02x|", c)
			}
		}
		if next == o {
			break
		}
		m = next
	}
	return res
}

func (o *Mbuf) GetRecord(timeSec float64, meta string) *MbufJson {
	return &MbufJson{Time: timeSec, Meta: meta, K12data: o.GetK12String(), K12PktLen: o.PktLen()}
}

//DumpK12  dump in k12 format
func (o *Mbuf) DumpK12(timeSec float64, file *os.File) {

	io.WriteString(file, "\n")
	io.WriteString(file, "+---------+---------------+----------+\n")
	mt := uint64(timeSec/60) % 60
	s := uint64(timeSec) % 60
	sm := uint64(reminder(timeSec) * 1000.0)
	su := uint64(reminder(timeSec*1000.0) * 1000.0)
	io.WriteString(file, fmt.Sprintf("00:%02d:%02d,%03d,%03d   ETHER \n", mt, s, sm, su))
	io.WriteString(file, fmt.Sprintf("|0   |%s\n\n", o.GetK12String()))
}
