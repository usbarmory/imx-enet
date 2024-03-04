// i.MX Ethernet (ENET) driver
//
// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package enet

import (
	"encoding/binary"
	"errors"
	"net"

	"github.com/usbarmory/tamago/soc/nxp/enet"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// NIC represents an virtual Ethernet instance.
type NIC struct {
	// MAC address
	MAC net.HardwareAddr

	// Link is a gVisor channel endpoint
	Link *channel.Endpoint

	// Device is the physical interface associated to the virtual one.
	Device *enet.ENET
}

type notification struct {
	eth *NIC
}

func (n *notification) WriteNotify() {
	n.eth.Device.Tx(n.eth.Tx())
}

// Init initializes a virtual Ethernet instance bound to a physical Ethernet
// device.
func (eth *NIC) Init() (err error) {
	if eth.Link == nil {
		return errors.New("missing link endpoint")
	}

	if len(eth.MAC) != 6 {
		return errors.New("invalid MAC address")
	}

	if eth.Device == nil {
		return
	}

	eth.Device.MAC = eth.MAC
	eth.Device.RxHandler = eth.Rx
	eth.Device.Init()

	eth.Link.AddNotify(&notification{
		eth: eth,
	})

	return
}

// Rx receives a single Ethernet frame from the virtual Ethernet instance.
func (eth *NIC) Rx(buf []byte) {
	if len(buf) < 14 {
		return
	}

	hdr := buf[0:14]
	proto := tcpip.NetworkProtocolNumber(binary.BigEndian.Uint16(buf[12:14]))
	payload := buf[14:]

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: len(hdr),
		Payload:            buffer.MakeWithData(payload),
	})

	copy(pkt.LinkHeader().Push(len(hdr)), hdr)

	eth.Link.InjectInbound(proto, pkt)

	return
}

// Tx transmits a single Ethernet frame to the virtual Ethernet instance.
func (eth *NIC) Tx() (buf []byte) {
	var pkt *stack.PacketBuffer

	if pkt = eth.Link.Read(); pkt.IsNil() {
		return
	}

	proto := make([]byte, 2)
	binary.BigEndian.PutUint16(proto, uint16(pkt.NetworkProtocolNumber))

	// Ethernet frame header
	buf = append(buf, pkt.EgressRoute.RemoteLinkAddress...)
	buf = append(buf, eth.MAC...)
	buf = append(buf, proto...)

	for _, v := range pkt.AsSlices() {
		buf = append(buf, v...)
	}

	return
}
