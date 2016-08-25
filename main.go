package main

import (
	"fmt"

	"github.com/docker/go-plugins-helpers/network"
	_ "github.com/vishvananda/netns"
)

func main() {

	driver := NewPipeNetworkDriver()
	handler := network.NewHandler(driver)
	fmt.Println(handler.ServeUnix("root", driver.Name))

	/*
		handler, err := netns.GetFromPath("/var/run/docker/netns/decf9feb2be9")
		if err != nil {
			fmt.Println("get ns handler failed", err)
		}
		fmt.Println("ns pid from netns is", int(handler))

		fd, err1 := syscall.Open("/var/run/docker/netns/decf9feb2be9", syscall.O_RDONLY, 0)
		if err1 != nil {
			fmt.Println("get ns pid failed", err1)
		}
		fmt.Println("ns pid from syscall is", fd)
	*/
}
