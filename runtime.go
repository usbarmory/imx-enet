// i.MX Ethernet (ENET) driver
//
// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package enet

import (
	"context"
	"errors"
	"net"
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

func (iface *Interface) Socket(ctx context.Context, network string, family, sotype int, laddr, raddr net.Addr) (c net.Conn, l net.Listener, err error) {
	var proto tcpip.NetworkProtocolNumber
	var lFullAddr tcpip.FullAddress
	var rFullAddr tcpip.FullAddress

	if laddr != nil && laddr.String() != "<nil>" {
		if lFullAddr, err = fullAddr(laddr.String()); err != nil {
			return
		}
	}

	if raddr != nil && raddr.String() != "<nil>" {
		if rFullAddr, err = fullAddr(raddr.String()); err != nil {
			return
		}
	}

	switch family {
	case syscall.AF_INET:
		proto = ipv4.ProtocolNumber
	default:
		return nil, nil, errors.New("unsupported address family")
	}

	switch network {
	case "udp":
		if sotype != syscall.SOCK_DGRAM {
			return nil, nil, errors.New("unsupported socket type")
		}

		conn, err := gonet.DialUDP(iface.Stack, &lFullAddr, &rFullAddr, proto)

		if err != nil {
			return nil, nil, err
		}

		c = (net.Conn)(conn)
	case "tcp":
		if sotype != syscall.SOCK_STREAM {
			return nil, nil, errors.New("unsupported socket type")
		}

		if raddr != nil {
			conn, err := gonet.DialContextTCP(ctx, iface.Stack, rFullAddr, proto)

			if err != nil {
				return nil, nil, err
			}

			c = (net.Conn)(conn)
		} else {
			listener, err := gonet.ListenTCP(iface.Stack, lFullAddr, ipv4.ProtocolNumber)

			if err != nil {
				return nil, nil, err
			}

			l = (net.Listener)(listener)
		}
	default:
		err = errors.New("unsupported network")
	}

	return
}
