// Copyright (c) 2013, Latermoon <lptmoon@gmail.com>
// All rights reserved.
//
// Go版RedisServer
// @author latermoon
// @since 2013-08-14
// @last 2013-09-07

package goredis

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/debug"
)

const (
	CR   = '\r'
	LF   = '\n'
	CRLF = "\r\n"
)

// 处理接收到的连接和数据
type ServerHandler interface {
	SessionOpened(session *Session)
	SessionClosed(session *Session, err error)
	On(session *Session, cmd *Command) (reply *Reply)
}

// ==============================
// RedisServer只实现最基本的Redis协议
// 提供On接口处理传入的各种指令，使用session返回数据
// ==============================
type RedisServer struct {
	// 指定的处理程序
	handler ServerHandler
}

func NewServer(handler ServerHandler) (server *RedisServer) {
	server = &RedisServer{}
	server.SetHandler(handler)
	return
}

func (server *RedisServer) SetHandler(handler ServerHandler) {
	server.handler = handler
}

/**
 * 开始监听主机端口
 * @param host "localhost:6379"
 */
func (server *RedisServer) Listen(host string) error {
	listener, err := net.Listen("tcp", host)
	if err != nil {
		return err
	}

	if server.handler == nil {
		return errors.New("[goredis] must call SetHandler(...) before Listen")
	}

	// run loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Stderr.WriteString(fmt.Sprint("[goredis] accepted error", err, "\n"))
			continue
		}
		// go
		go server.handleConnection(NewSession(conn))
	}
	return nil
}

// 处理一个客户端连接
func (server *RedisServer) handleConnection(session *Session) {
	// 异常处理
	defer func() {
		if v := recover(); v != nil {
			os.Stderr.WriteString(fmt.Sprintf("[goredis] fatal %s %s\n%s\n", session.RemoteAddr(), v, string(debug.Stack())))
			session.Close()
			// callback
			err, ok := v.(error)
			if !ok {
				err = errors.New(fmt.Sprint(v))
			}
			server.handler.SessionClosed(session, err)
		}
	}()

	server.handler.SessionOpened(session)

	var lastErr error
	for {
		var cmd *Command
		cmd, lastErr = session.ReadCommand()
		// 常见的error是:
		// 1) io.EOF
		// 2) read tcp 127.0.0.1:51863: connection reset by peer
		if lastErr != nil {
			session.Close()
			break
		}
		// 处理
		reply := server.handler.On(session, cmd)
		if reply != nil {
			lastErr = session.Reply(reply)
			if lastErr != nil {
				session.Close()
				break
			}
		}
	}

	server.handler.SessionClosed(session, lastErr)
}
