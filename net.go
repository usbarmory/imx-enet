// i.MX Ethernet (ENET) driver
//
// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package enet implements TCP/IP connectivity through Ethernet (ENET)
// controllers on i.MX6 SoCs.
//
// The TCP/IP stack is implemented using gVisor pure Go implementation.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm` as
// supported by the TamaGo framework for bare metal Go on ARM SoCs, see
// https://github.com/usbarmory/tamago.
package enet

import (
	"fmt"
	"net"
	"strconv"

	"github.com/usbarmory/tamago/soc/nxp/enet"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// MTU represents the Ethernet Maximum Transmission Unit.
var MTU uint32 = enet.MTU

// Interface represents an Ethernet interface instance.
type Interface struct {
	address tcpip.Address
	gateway tcpip.Address

	nicid tcpip.NICID
	NIC   *NIC

	Stack *stack.Stack
	Link  *channel.Endpoint
}

func (iface *Interface) OnNeighborAdded(nicid tcpip.NICID, entry stack.NeighborEntry) {
	if entry.Addr == iface.gateway && len(entry.LinkAddr) > 0 {
		iface.NIC.Gateway = entry.LinkAddr
	}
}

func (iface *Interface) OnNeighborChanged(nicid tcpip.NICID, entry stack.NeighborEntry) {
	if entry.Addr == iface.gateway && len(entry.LinkAddr) > 0 {
		iface.NIC.Gateway = entry.LinkAddr
	}
}

func (iface *Interface) OnNeighborRemoved(nicid tcpip.NICID, entry stack.NeighborEntry) {
	if entry.Addr == iface.gateway {
		iface.NIC.Gateway = header.EthernetBroadcastAddress
	}
}

func (iface *Interface) configure(mac string) (err error) {
	iface.Stack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			icmp.NewProtocol4},
		NUDDisp: iface,
	})

	linkAddr, err := tcpip.ParseMACAddress(mac)

	if err != nil {
		return
	}

	iface.Link = channel.New(256, MTU, linkAddr)
	linkEP := stack.LinkEndpoint(iface.Link)
	iface.Link.LinkEPCapabilities |= stack.CapabilityResolutionRequired

	if err := iface.Stack.CreateNIC(iface.nicid, linkEP); err != nil {
		return fmt.Errorf("%v", err)
	}

	protocolAddr := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: iface.address.WithPrefix(),
	}

	if err := iface.Stack.AddProtocolAddress(iface.nicid, protocolAddr, stack.AddressProperties{}); err != nil {
		return fmt.Errorf("%v", err)
	}

	rt := iface.Stack.GetRouteTable()

	rt = append(rt, tcpip.Route{
		Destination: header.IPv4EmptySubnet,
		Gateway:     iface.gateway,
		NIC:         iface.nicid,
	})

	iface.Stack.SetRouteTable(rt)

	return
}

// EnableICMP adds an ICMP endpoint to the interface, it is useful to enable
// ping requests.
func (iface *Interface) EnableICMP() error {
	var wq waiter.Queue

	ep, err := iface.Stack.NewEndpoint(icmp.ProtocolNumber4, ipv4.ProtocolNumber, &wq)

	if err != nil {
		return fmt.Errorf("endpoint error (icmp): %v", err)
	}

	fullAddr := tcpip.FullAddress{Addr: iface.address, Port: 0, NIC: iface.nicid}

	if err := ep.Bind(fullAddr); err != nil {
		return fmt.Errorf("bind error (icmp endpoint): ", err)
	}

	return nil
}

// ListenerTCP4 returns a net.Listener capable of accepting IPv4 TCP
// connections for the argument port on the Ethernet interface.
func (iface *Interface) ListenerTCP4(port uint16) (net.Listener, error) {
	fullAddr := tcpip.FullAddress{Addr: iface.address, Port: port, NIC: iface.nicid}

	listener, err := gonet.ListenTCP(iface.Stack, fullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Listener)(listener), nil
}

// Dial connects to an IPv4 TCP address, over the Ethernet interface.
func (iface *Interface) DialTCP4(address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)

	if err != nil {
		return nil, err
	}

	p, err := strconv.Atoi(port)

	if err != nil {
		return nil, err
	}

	addr := net.ParseIP(host)
	fullAddr := tcpip.FullAddress{Addr: tcpip.Address(addr.To4()), Port: uint16(p)}

	conn, err := gonet.DialTCP(iface.Stack, fullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Conn)(conn), nil
}

// Init initializes an Ethernet interface.
func Init(nic *enet.ENET, ip string, mac string, gateway string, id int) (iface *Interface, err error) {
	address, err := net.ParseMAC(mac)

	if err != nil {
		return
	}

	iface = &Interface{
		nicid:   tcpip.NICID(1),
		address: tcpip.Address(net.ParseIP(ip)).To4(),
		gateway: tcpip.Address(net.ParseIP(gateway)).To4(),
	}

	if err = iface.configure(mac); err != nil {
		return
	}

	iface.nicid = tcpip.NICID(id)

	iface.NIC = &NIC{
		MAC:     address,
		Link:    iface.Link,
		Device:  nic,
		Gateway: header.EthernetBroadcastAddress,
	}

	err = iface.NIC.Init()

	return
}
