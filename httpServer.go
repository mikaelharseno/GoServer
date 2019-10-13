package main

import (
	"./http"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
)

const usageMsg = "Usage: ./httpserver --files www_directory/ --port 8000 [--num-threads 5]\n" +
	"       ./httpserver --proxy inst.eecs.berkeley.edu:80 --port 8000 [--num-threads 5]\n"
const maxQueueSize = 50

var (
	serverFilesDirectory string
	proxyAddress         string
	proxyPort            int
	isProxy int
	numTh int

	interrupted chan os.Signal
	mainkill chan int
	otherkill chan int
	died chan int
)

func handleFilesRequest(connection net.Conn) {
	// TODO Fill this in to complete Task #2
	req, err := http.ParseRequest(connection)
	if err != nil {
		fmt.Fprintf(os.Stderr, "File http request parse error. \n")
	}

	//Path resolution
	var completepath string
	completepath = serverFilesDirectory + req.Path

	if req.Method == "GET" {
		content, err := ioutil.ReadFile(completepath)
		if err != nil {
			// Not a file
			fmt.Fprintf(os.Stderr, "File not found. \n")
			files, err := ioutil.ReadDir(completepath)
			if err != nil {
				//Not a file, not a directory. Not found error
				http.StartResponse(connection, 404)
				http.EndHeaders(connection)
				connection.Close()
				return
			}

			completepath = completepath + "/index.html"
			content, err := ioutil.ReadFile(completepath)
			if err != nil {
				dircontent := "<a href=\"../\">Parent directory</a>\r\n"
				for _, file := range files {
					dircontent = dircontent + "<a href=\"./" + file.Name() + "\">" + file.Name() + "</a>\r\n"
				}
				http.StartResponse(connection, 200)
				http.SendHeader(connection, "Content-Type", http.GetMimeType(completepath))
				http.SendHeader(connection, "Content-Length", fmt.Sprintf("%d", len(dircontent)))
				http.EndHeaders(connection)
				http.SendString(connection, fmt.Sprintf("%s", dircontent))
				connection.Close()
				return
			}

			http.StartResponse(connection, 200)
			http.SendHeader(connection, "Content-Type", http.GetMimeType(completepath))
			http.SendHeader(connection, "Content-Length", fmt.Sprintf("%d", len(content)))
			http.EndHeaders(connection)
			http.SendData(connection, content)
			connection.Close()
			return
		}

		http.StartResponse(connection, 200)
		http.SendHeader(connection, "Content-Type", http.GetMimeType(completepath))
		http.SendHeader(connection, "Content-Length", fmt.Sprintf("%d", len(content)))
		http.EndHeaders(connection)
		http.SendData(connection, content)
		connection.Close()
		return
	}
	connection.Close()
}

func threadServerToClient(clientConn net.Conn, serverConn net.Conn, wg *sync.WaitGroup) {
	//From client to server
	fmt.Fprintf(os.Stderr, "	Getting new client. \n")
	io.Copy(serverConn, clientConn)
	fmt.Fprintf(os.Stderr, "	Waiting for done. \n")
	serverConn.Close()
	clientConn.Close()
	wg.Done()
	fmt.Fprintf(os.Stderr, "	Done. Waiting for new client. \n")
}

func threadClientToServer(clientConn net.Conn, serverConn net.Conn, wg *sync.WaitGroup) {
	//From client to server
	io.Copy(clientConn, serverConn)
	fmt.Fprintf(os.Stderr, "Starting wait. \n")
	serverConn.Close()
	clientConn.Close()
	wg.Wait()
	fmt.Fprintf(os.Stderr, "Finished connection. \n")
}

func handleProxyRequest(clientConn net.Conn) {
}

func handleSigInt() {
	// TODO Fill this in to help complete Task #4
	// You should run this in its own goroutine
	// Perform a blocking receive on the 'interrupted' channel
	// Then, clean up the main and worker goroutines
	// Finally, when all goroutines have cleanly finished, exit the process

	<-interrupted
	count := numTh
	for i:= 1; i < numTh*5; i++ {
		otherkill<-1
	}
	mainkill<-1

	for (count < numTh + 1) {
		<-died
		count = count + 1
	}

	os.Exit(-1)
}

func initWorkerPool(numThreads int, requestHandler func(net.Conn), passchannel chan net.Conn) {
	// TODO Fill this in as part of Task #1
	// Create a fixed number of goroutines to handle requests
	fmt.Fprintf(os.Stderr, "Initializing worker pool. \n")
	for i:= 0; i < numThreads; i++ {
		go workerThread(passchannel, requestHandler)
	}
}

func workerThread(conns <-chan net.Conn, requestHandler func(net.Conn)) {
	fmt.Fprintf(os.Stderr, "Worker thread started. \n")
	if (isProxy == 1) {
		for newconn := range conns {
			select {
			case <-otherkill:
				died<-1
				return
			default:
				dialto := proxyAddress + fmt.Sprintf(":%d", proxyPort)
				realserverconn, err := net.Dial("tcp", dialto)
				if err != nil {
					// handle error
					fmt.Fprintf(os.Stderr, "Proxy server dial failed. \n")
					fmt.Fprintf(os.Stderr, "Was trying to connect to: \n")
					fmt.Fprintf(os.Stderr, dialto)
					//continue
				}

				fmt.Fprintf(os.Stderr, "Getting new connection. \n")

				var wg sync.WaitGroup
				wg.Add(1)
				go threadServerToClient(newconn, realserverconn, &wg)
				threadClientToServer(newconn, realserverconn, &wg)
			}
		}
	} else {
		for newconn := range conns {
			select {
			case <-otherkill:
				died<-1
				return
			default:
				fmt.Fprintf(os.Stderr, "Getting new connection. \n")
				requestHandler(newconn)
				fmt.Fprintf(os.Stderr, "Finished connection. \n")
			}
		}
	}
}

func serveForever(numThreads int, port int, requestHandler func(net.Conn)) {
	// TODO Fill this in as part of Task #1
	// Create a Listener and accept client connections
	// Pass connections to the thread pool via a channel
	//mess :=
	numTh = numThreads
	go handleSigInt();
	fmt.Fprintf(os.Stderr, "Starting worker pool. \n")
	passchannel := make(chan net.Conn, maxQueueSize)
	initWorkerPool(numThreads, requestHandler, passchannel)
	s := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", s)
	if err != nil {
		//exitWithUsage()
	}
	for {
		select {
		case <-mainkill:
			died<-1
			return
		default:
			conn, err := ln.Accept()
			fmt.Fprintf(os.Stderr, "Accepted new connection. \n")
			if err != nil {// handle error
				fmt.Fprintf(os.Stderr, "Connection accept error. \n")
			}
			fmt.Fprintf(os.Stderr, "Passing connection to worker. \n")
			passchannel<-conn
		}
	}
}

func exitWithUsage() {
	fmt.Fprintf(os.Stderr, usageMsg)
	os.Exit(-1)
}

func main() {
	// Command line argument parsing
	var requestHandler func(net.Conn)
	var serverPort int
	numThreads := 1
	isProxy = 0
	var err error

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--files":
			requestHandler = handleFilesRequest
			if i == len(os.Args)-1 {
				fmt.Fprintln(os.Stderr, "Expected argument after --files")
				exitWithUsage()
			}
			serverFilesDirectory = os.Args[i+1]
			i++

		case "--proxy":
			requestHandler = handleProxyRequest
			if i == len(os.Args)-1 {
				fmt.Fprintln(os.Stderr, "Expected argument after --proxy")
				exitWithUsage()
			}
			proxyTarget := os.Args[i+1]
			isProxy = 1
			i++

			tokens := strings.SplitN(proxyTarget, ":", 2)
			proxyAddress = tokens[0]
			proxyPort, err = strconv.Atoi(tokens[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "Expected integer for proxy port")
				exitWithUsage()
			}

		case "--port":
			if i == len(os.Args)-1 {
				fmt.Fprintln(os.Stderr, "Expected argument after --port")
				exitWithUsage()
			}

			portStr := os.Args[i+1]
			i++
			serverPort, err = strconv.Atoi(portStr)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Expected integer value for --port argument")
				exitWithUsage()
			}

		case "--num-threads":
			if i == len(os.Args)-1 {
				fmt.Fprintln(os.Stderr, "Expected argument after --num-threads")
				exitWithUsage()
			}
			numThreadsStr := os.Args[i+1]
			i++
			numThreads, err = strconv.Atoi(numThreadsStr)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Expected positive integer value for --num-threads argument")
				exitWithUsage()
			}

		case "--help":
			fmt.Printf(usageMsg)
			os.Exit(0)

		default:
			fmt.Fprintf(os.Stderr, "Unexpected command line argument %s\n", os.Args[i])
			exitWithUsage()
		}
	}

	if requestHandler == nil {
		fmt.Fprintln(os.Stderr, "Must specify one of either --files or --proxy")
		exitWithUsage()
	}

	// Set up a handler for SIGINT, used in Task #4
	interrupted = make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	serveForever(numThreads, serverPort, requestHandler)
}
