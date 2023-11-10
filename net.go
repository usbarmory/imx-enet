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
	"syscall"

	"github.com/usbarmory/tamago/soc/nxp/enet"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
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
	NICID tcpip.NICID
	NIC   *NIC

	Stack *stack.Stack
	Link  *channel.Endpoint

	protos map[int]tcpip.NetworkProtocolNumber
}

var DefaultStackOptions = &stack.Options{
	NetworkProtocols: []stack.NetworkProtocolFactory{
		arp.NewProtocol,
		ipv4.NewProtocol,
	}, TransportProtocols: []stack.TransportProtocolFactory{
		tcp.NewProtocol,
		icmp.NewProtocol4,
		udp.NewProtocol},
}

func (iface *Interface) configure(opts Options) (err error) {
	iface.Stack = stack.New(*opts.StackOptions)
	iface.Link = channel.New(256, MTU, opts.MAC)
	linkEP := stack.LinkEndpoint(iface.Link)
	iface.Link.LinkEPCapabilities |= stack.CapabilityResolutionRequired

	if err := iface.Stack.CreateNIC(iface.NICID, linkEP); err != nil {
		return fmt.Errorf("%v", err)
	}

	rt := iface.Stack.GetRouteTable()
	if c := opts.IPv4; c != nil {
		r, err := iface.configureProtocol(ipv4.ProtocolNumber, c)
		if err != nil {
			return fmt.Errorf("failed to configure IPv4: %v", err)
		}
		rt = append(rt, r...)
		iface.protos[syscall.AF_INET] = ipv4.ProtocolNumber
	}
	if c := opts.IPv6; c != nil {
		r, err := iface.configureProtocol(ipv6.ProtocolNumber, c)
		if err != nil {
			return fmt.Errorf("failed to configure IPv6: %v", err)
		}
		rt = append(rt, r...)
		iface.protos[syscall.AF_INET6] = ipv6.ProtocolNumber
	}
	iface.Stack.SetRouteTable(rt)

	return
}

func (iface *Interface) configureProtocol(p tcpip.NetworkProtocolNumber, cfg *IPConfig) ([]tcpip.Route, error) {
	protocolAddr := tcpip.ProtocolAddress{
		Protocol:          p,
		AddressWithPrefix: cfg.Address,
	}
	if err := iface.Stack.AddProtocolAddress(iface.NICID, protocolAddr, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("%v", err)
	}
	rt := []tcpip.Route{
		tcpip.Route{Destination: protocolAddr.AddressWithPrefix.Subnet(),
			NIC: iface.NICID,
		},
	}

	if !cfg.Gateway.Unspecified() {
		dst, ok := map[tcpip.NetworkProtocolNumber]tcpip.Subnet{
			ipv4.ProtocolNumber: header.IPv4EmptySubnet,
			ipv6.ProtocolNumber: header.IPv6EmptySubnet,
		}[p]
		if ok {
			rt = append(rt, tcpip.Route{
				Destination: dst,
				Gateway:     cfg.Gateway,
				NIC:         iface.NICID,
			})
		}
	}
	return rt, nil
}

// EnableICMP adds an ICMP endpoint to the interface, it is useful to enable
// ping requests.
func (iface *Interface) EnableICMP() error {
	var wq waiter.Queue

	ep, err := iface.Stack.NewEndpoint(icmp.ProtocolNumber4, ipv4.ProtocolNumber, &wq)

	if err != nil {
		return fmt.Errorf("endpoint error (icmp): %v", err)
	}

	addr, tcpErr := iface.Stack.GetMainNICAddress(iface.NICID, ipv4.ProtocolNumber)

	if tcpErr != nil {
		return fmt.Errorf("couldn't get NIC IP address: %v", tcpErr)
	}

	fullAddr := tcpip.FullAddress{Addr: addr.Address, Port: 0, NIC: iface.NICID}

	if err := ep.Bind(fullAddr); err != nil {
		return fmt.Errorf("bind error (icmp endpoint): ", err)
	}

	return nil
}

// ListenerTCP4 returns a net.Listener capable of accepting IPv4 TCP
// connections for the argument port.
func (iface *Interface) ListenerTCP4(port uint16) (net.Listener, error) {
	addr, tcpErr := iface.Stack.GetMainNICAddress(iface.NICID, ipv4.ProtocolNumber)

	if tcpErr != nil {
		return nil, fmt.Errorf("couldn't get NIC IP address: %v", tcpErr)
	}

	fullAddr := tcpip.FullAddress{Addr: addr.Address, Port: port, NIC: iface.NICID}
	listener, err := gonet.ListenTCP(iface.Stack, fullAddr, ipv4.ProtocolNumber)

	if err != nil {
		return nil, err
	}

	return (net.Listener)(listener), nil
}

// DialTCP4 connects to an IPv4 TCP address.
func (iface *Interface) DialTCP4(address string) (net.Conn, error) {
	return iface.DialContextTCP4(context.Background(), address)
}

// DialContextTCP4 connects to an IPv4 TCP address with support for timeout
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
	var lFullAddr tcpip.FullAddress
	var rFullAddr tcpip.FullAddress
	var err error

	if lAddr != "" {
		if lFullAddr, err = fullAddr(lAddr); err != nil {
			return nil, fmt.Errorf("failed to parse lAddr %q: %v", lAddr, err)
		}
	}

	if rAddr != "" {
		if rFullAddr, err = fullAddr(rAddr); err != nil {
			return nil, fmt.Errorf("failed to parse rAddr %q: %v", rAddr, err)
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
	var p int

	host, port, err := net.SplitHostPort(a)

	if err == nil {
		if p, err = strconv.Atoi(port); err != nil {
			return tcpip.FullAddress{}, err
		}
	} else {
		host = a
	}

	addr := net.ParseIP(host)
	return tcpip.FullAddress{Addr: tcpip.AddrFromSlice(addr.To4()), Port: uint16(p)}, nil
}

// Init initializes an Ethernet interface.
func Init(nic *enet.ENET, ip string, netmask string, mac string, gateway string, id int) (iface *Interface, err error) {
	address, err := tcpip.ParseMACAddress(mac)
	if err != nil {
		return
	}

	return InitWithOptions(nic, tcpip.NICID(id), Options{
		MAC: address,
		IPv4: &IPConfig{
			Address: tcpip.AddressWithPrefix{
				Address:   tcpip.AddrFromSlice(net.ParseIP(ip).To4()),
				PrefixLen: tcpip.MaskFromBytes(net.ParseIP(netmask).To4()).Prefix(),
			},
			Gateway: tcpip.AddrFromSlice(net.ParseIP(gateway)).To4(),
		},
	})
}

// IPConfig holds IP config information.
type IPConfig struct {
	Address tcpip.AddressWithPrefix
	Gateway tcpip.Address
}

// Options contains parameters for configuring the network.
type Options struct {
	// MAC is the link layer address to set on the stack.
	MAC tcpip.LinkAddress
	// StackOptions can optionally be used to pass in custom options when
	// creating the stack. If unset, a default IPv4-only stack will be created.
	StackOptions *stack.Options
	// IPv4 contains configuration for the IPv4 protocol.
	IPv4 *IPConfig
	// IPv6 contains configuration for the IPv6 protocol.
	IPv6 *IPConfig
}

// InitWithOptions creates a new Interface with the provided options.
// This method allows for more control over the configuration of the TCP/IP stack which is
// created.
func InitWithOptions(nic *enet.ENET, id tcpip.NICID, opts Options) (*Interface, error) {
	if opts.StackOptions == nil {
		opts.StackOptions = DefaultStackOptions
	}

	iface := &Interface{
		NICID:  id,
		protos: make(map[int]tcpip.NetworkProtocolNumber),
	}

	if err := iface.configure(opts); err != nil {
		return nil, err
	}

	iface.NIC = &NIC{
		MAC:    net.HardwareAddr(opts.MAC),
		Link:   iface.Link,
		Device: nic,
	}

	return iface, iface.NIC.Init()
}
