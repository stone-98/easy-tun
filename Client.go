package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"github.com/fatih/color"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/songgao/water"
)

var (
	inSer = flag.String("ser", "localhost", "server address")
	inDev = flag.String("dev", "gtun", "local tun device name")
)

func main() {
	flag.Parse()

	// 创建tun网卡
	config := water.Config{
		DeviceType: water.TUN,
	}
	// windows os是config.InterfaceName
	config.Name = *inDev
	ifce, err := water.New(config)
	if err != nil {
		color.Red(err.Error())
		return
	}
	// 连接server，默认端口9621
	conn, err := connServer(*inSer + ":9621")
	if err != nil {
		color.Red(err.Error())
		return
	}

	color.Red("server address		:%s", *inSer)
	color.Red("local tun device name :%s", *inDev)
	color.Red("connect server succeed.")

	// 读取tun网卡，将读取到的数据转发至server端
	go ifceRead(ifce, conn)
	// 接收server端的数据，并将数据写到tun网卡中
	go ifceWrite(ifce, conn)

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGABRT, syscall.SIGHUP)
	<-sig
}

// 连接server
func connServer(srv string) (conn net.Conn, err error) {
	conn, err = net.Dial("tcp", srv)
	if err != nil {
		return nil, err
	}
	return conn, err
}

// 读取tun网卡数据转发到server端
func ifceRead(ifce *water.Interface, conn net.Conn) {
	packet := make([]byte, 2048)
	for {
		// 从tun网卡读取数据
		size, err := ifce.Read(packet)
		if err != nil {
			color.Red(err.Error())
			break
		}
		// 转发到server端
		err = forwardSer(conn, packet[:size])
		if err != nil {
			color.Red(err.Error())
		}
	}
}

// 将server端的数据读取出来写到tun网卡
func ifceWrite(ifce *water.Interface, conn net.Conn) {
	// 定义SplitFunc，解决tcp的粘贴包问题
	splitFunc := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		// 检查 atEOF 参数和数据包头部的四个字节是否为 0x123456
		if !atEOF && len(data) > 6 && binary.BigEndian.Uint32(data[:4]) == 0x123456 {
			// 数据的实际大小
			var size int16
			// 读出数据包中实际数据的大小(大小为 0 ~ 2^16)
			binary.Read(bytes.NewReader(data[4:6]), binary.BigEndian, &size)
			// 总大小 = 数据的实际长度+魔数+长度标识
			allSize := int(size) + 6
			// 如果总大小小于等于数据包的大小，则不做处理！
			if allSize <= len(data) {
				return allSize, data[:allSize], nil
			}
		}
		return
	}
	// 创建buffer
	buf := bytes.NewBuffer(nil)
	// 定义包，由于标识数据包长度的只有两个字节故数据包最大为 2^16+4(魔数)+2(长度标识)
	packet := make([]byte, 65542)
	for {
		nr, err := conn.Read(packet[0:])
		buf.Write(packet[0:nr])
		if err != nil {
			if err == io.EOF {
				continue
			} else {
				color.Red(err.Error())
				break
			}
		}
		scanner := bufio.NewScanner(buf)
		scanner.Split(splitFunc)
		for scanner.Scan() {
			_, err = ifce.Write(scanner.Bytes()[6:])
			if err != nil {
				color.Red(err.Error())
			}
		}
		buf.Reset()
	}
}

// 将tun的数据包写到server端
func forwardSer(srvcon net.Conn, buff []byte) (err error) {
	output := make([]byte, 0)
	magic := make([]byte, 4)
	binary.BigEndian.PutUint32(magic, 0x123456)
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(buff)))

	// magic
	output = append(output, magic...)
	// length
	output = append(output, length...)
	// data
	output = append(output, buff...)

	left := len(output)
	for left > 0 {
		nw, er := srvcon.Write(output)
		if err != nil {
			err = er
		}
		left -= nw
	}
	return err
}
