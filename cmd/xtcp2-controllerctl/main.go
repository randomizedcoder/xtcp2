package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	target := flag.String("controller", "localhost:9443", "controller address")
	name := flag.String("serverName", "", "TLS server name")
	cert := flag.String("cert", "", "operator certificate")
	key := flag.String("key", "", "operator key")
	ca := flag.String("ca", "", "server CA")
	method := flag.String("method", "inspect", "propose, analyze, review, control, inspect")
	file := flag.String("file", "", "protobuf-text request file (empty InspectRequest if omitted)")
	flag.Parse()
	tls, e := identity.TLSConfig(*cert, *key, *ca, *name, false)
	if e != nil {
		return e
	}
	conn, e := grpc.NewClient(*target, grpc.WithTransportCredentials(credentials.NewTLS(tls)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(treatment.MaxStateBytes)))
	if e != nil {
		return e
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("close: %v", cerr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client := pb.NewOperatorServiceClient(conn)
	var req, response proto.Message
	switch *method {
	case "propose":
		req = new(pb.ProposeRequest)
	case "review":
		req = new(pb.ReviewRequest)
	case "analyze":
		req = new(pb.AnalyzeRequest)
	case "control":
		req = new(pb.ControlRequest)
	case "inspect":
		req = new(pb.InspectRequest)
	default:
		return fmt.Errorf("unknown method %q", *method)
	}
	if *file != "" {
		b, e := os.ReadFile(*file)
		if e != nil {
			return e
		}
		if e = prototext.Unmarshal(b, req); e != nil {
			return e
		}
	}
	switch r := req.(type) {
	case *pb.ProposeRequest:
		response, e = client.Propose(ctx, r)
	case *pb.ReviewRequest:
		response, e = client.Review(ctx, r)
	case *pb.AnalyzeRequest:
		response, e = client.Analyze(ctx, r)
	case *pb.ControlRequest:
		response, e = client.Control(ctx, r)
	case *pb.InspectRequest:
		response, e = client.Inspect(ctx, r)
	}
	if e != nil {
		return e
	}
	fmt.Print(prototext.Format(response))
	return nil
}
