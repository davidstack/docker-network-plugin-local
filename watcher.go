package main

import (
	"fmt"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

type Watcher struct {
	client *docker.Client
}

func NewWatcher() (Watcher, error) {
	w := Watcher{client: getDockerClient()}
	return w, nil
}
func (w *Watcher) ContainerStarted(id string) {
	info, err := w.client.InspectContainer(id)
	if err != nil {
		fmt.Println("ContainerStarted", "error inspecting container %s: %s", id, err)
		return
	}
	// check that it's on our network
	for _, net := range info.NetworkSettings.Networks {
		endpoint, localnetwork, err := findNetworkInfo(net.NetworkID, net.EndpointID)
		if err != nil {
			fmt.Println("ContainerStarted", "unable to find network %s info: %s", net.NetworkID, err)
			continue
		} else {
			configContainerIp(endpoint, localnetwork)
		}
	}
}

func (w *Watcher) ContainerDied(id string) {

}

func (w *Watcher) ContainerDestroyed(id string) {

}
func (w *Watcher) StartWatch() {
	go w.Watching()

}
func (w *Watcher) Watching() {
	defer func() {
		if r := recover(); r != nil {
			//restart watching
			fmt.Println("Create Container failed", r)
		}
	}()
	client := getDockerClient()
	events := make(chan *docker.APIEvents)
	client.AddEventListener(events)
	fmt.Println("start watching...")
	for msg := range events {
		//fmt.Println("event:", msg.ID[:12], msg.Status)
		if msg.Type == "container" {
			fmt.Println("event is not container " + msg.ID[:12] + " " + msg.Action)
			switch msg.Action {
			case "start":
				w.ContainerStarted(msg.ID[:12])
			case "stop":
				w.ContainerDied(msg.ID[:12])
			case "destory":
				w.ContainerDestroyed(msg.ID[:12])
			}
		}
	}
}
func getDockerClient() *docker.Client {
	for {
		client, err := docker.NewClient("unix:///var/run/docker.sock")
		if err != nil {
			fmt.Println("get docker client failed, try again", err)
			time.Sleep(1000)
			continue
		} else {
			return client
		}
	}

}
