# docker-network-plugin-local
with this docker network plugin,the container can use the ip address of the host network.

#install
 run the binary in you docker host,start the binary before you create local network

#useage
 0.192.168.159.0/24 is your docker host network
 1. docker network create --driver=local  --gateway=192.168.159.2 --subnet=192.168.159.0/24 local 
 2. docker run -itd --ip=192.168.159.140 --net=local 10.110.17.138:5000/centos:6.7
