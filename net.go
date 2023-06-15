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
	"context"
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
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// MTU represents the Ethernet Maximum Transmission Unit.
var MTU uint32 = enet.MTU

// Interface represents an Ethernet interface instance.
type Interface struct {
	nicid tcpip.NICID
	NIC   *NIC

	Stack *stack.Stack
	Link  *channel.Endpoint
}

func (iface *Interface) configure(mac string, ip tcpip.AddressWithPrefix, gw tcpip.Address) (err error) {
	iface.Stack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			icmp.NewProtocol4,
			udp.NewProtocol},
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
		AddressWithPrefix: ip,
	}

	if err := iface.Stack.AddProtocolAddress(iface.nicid, protocolAddr, stack.AddressProperties{}); err != nil {
		return fmt.Errorf("%v", err)
	}

	rt := iface.Stack.GetRouteTable()

	rt = append(rt, tcpip.Route{
		Destination: protocolAddr.AddressWithPrefix.Subnet(),
		NIC:         iface.nicid,
	})

	rt = append(rt, tcpip.Route{
		Destination: header.IPv4EmptySubnet,
		Gateway:     gw,
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

	addr, tcpErr := iface.Stack.GetMainNICAddress(iface.nicid, ipv4.ProtocolNumber)

	if tcpErr != nil {
		return fmt.Errorf("couldn't get NIC IP address: %v", tcpErr)
	}

	fullAddr := tcpip.FullAddress{Addr: addr.Address, Port: 0, NIC: iface.nicid}

	if err := ep.Bind(fullAddr); err != nil {
		return fmt.Errorf("bind error (icmp endpoint): ", err)
	}

	return nil
}

// ListenerTCP4 returns a net.Listener capable of accepting IPv4 TCP
// connections for the argument port on the Ethernet interface.
func (iface *Interface) ListenerTCP4(port uint16) (net.Listener, error) {
	addr, tcpErr := iface.Stack.GetMainNICAddress(iface.nicid, ipv4.ProtocolNumber)

	if tcpErr != nil {
		return nil, fmt.Errorf("couldn't get NIC IP address: %v", tcpErr)
	}

	fullAddr := tcpip.FullAddress{Addr: addr.Address, Port: port, NIC: iface.nicid}
	listener, err := gonet.ListenTCP(iface.Stack, fullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Listener)(listener), nil
}

func (iface *Interface) DialTCP4(address string) (net.Conn, error) {
	return iface.DialContextTCP4(context.Background(), address)
}

// Dial connects to an IPv4 TCP address, over the Ethernet interface, with support for timeout
// supplied by ctx.
func (iface *Interface) DialContextTCP4(ctx context.Context, address string) (net.Conn, error) {
	fullAddr, err := fullAddr(address)

	if err != nil {
		return nil, err
	}

	conn, err := gonet.DialContextTCP(ctx, iface.Stack, fullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Conn)(conn), nil
}

// DialUDP4 creates a UDP connection to the ip:port specified by rAddr, optionally setting
// the local ip:port to lAddr.
func (iface *Interface) DialUDP4(lAddr, rAddr string) (net.Conn, error) {
	rFullAddr, err := fullAddr(rAddr)

	if err != nil {
		return nil, fmt.Errorf("failed to parse rAddr %q: %v", rAddr, err)
	}

	var lFullAddr tcpip.FullAddress

	if lAddr != "" {
		if lFullAddr, err = fullAddr(lAddr); err != nil {
			return nil, fmt.Errorf("failed to parse lAddr %q: %v", lAddr, err)
		}
	}

	conn, err := gonet.DialUDP(iface.Stack, &lFullAddr, &rFullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Conn)(conn), nil
}

// fullAddr attempts to convert the ip:port to a FullAddress struct.
func fullAddr(a string) (tcpip.FullAddress, error) {
	host, port, err := net.SplitHostPort(a)

	if err != nil {
		return tcpip.FullAddress{}, err
	}

	p, err := strconv.Atoi(port)

	if err != nil {
		return tcpip.FullAddress{}, err
	}

	addr := net.ParseIP(host)
	return tcpip.FullAddress{Addr: tcpip.AddrFromSlice(addr.To4()), Port: uint16(p)}, nil
}

// Init initializes an Ethernet interface.
func Init(nic *enet.ENET, ip string, netmask string, mac string, gateway string, id int) (iface *Interface, err error) {
	address, err := net.ParseMAC(mac)

	if err != nil {
		return
	}

	iface = &Interface{
		nicid: tcpip.NICID(id),
	}

	ipAddr := tcpip.AddressWithPrefix{
		Address:   tcpip.AddrFromSlice(net.ParseIP(ip).To4()),
		PrefixLen: tcpip.MaskFromBytes(net.ParseIP(netmask).To4()).Prefix(),
	}

	gwAddr := tcpip.AddrFromSlice(net.ParseIP(gateway)).To4()

	if err = iface.configure(mac, ipAddr, gwAddr); err != nil {
		return
	}

	iface.NIC = &NIC{
		MAC:    address,
		Link:   iface.Link,
		Device: nic,
	}

	err = iface.NIC.Init()

	return
}
