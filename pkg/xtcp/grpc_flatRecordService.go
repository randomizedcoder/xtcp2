package xtcp

import (
	"context"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/grpc"
)

type xtcpFlatRecordService struct {
	xtcp_flat_record.UnimplementedXTCPFlatRecordServiceServer

	ctx context.Context

	FlatRecordsClients sync.Map
	frStoreCount       atomic.Uint64
	frDeleteCount      atomic.Uint64

	PollFlatRecordsClients sync.Map
	pfrStoreCount          atomic.Uint64
	pfrDeleteCount         atomic.Uint64

	FlatRecordsResponsePool sync.Pool

	pollRequestCh *chan struct{}

	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec

	debugLevel uint32
}

func NewXtcpFlatRecordService(ctx context.Context, pollRequestCh *chan struct{}, debugLevel uint32) *xtcpFlatRecordService {

	s := new(xtcpFlatRecordService)

	s.debugLevel = debugLevel
	s.ctx = ctx

	s.FlatRecordsResponsePool = sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.FlatRecordsResponse)
		},
	}

	s.pollRequestCh = pollRequestCh

	s.pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp_record_grpc",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	s.pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_record_grpc",
			Name:      "histograms",
			Help:      "xtcp historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)

	return s
}

// FlatRecords is the GRPC handler.  This accepts the client,
// stores the pointer to the client's "stream" in the map, which allows
// other goroutines to send data on the stream.
// Then this goroutine just blocks waiting for the client to disconnect,
// or the context to be cancelled()
func (s *xtcpFlatRecordService) FlatRecords(
	flatRecordsReq *xtcp_flat_record.FlatRecordsRequest,
	stream xtcp_flat_record.XTCPFlatRecordService_FlatRecordsServer) error {

	startTime := time.Now()
	defer func() {
		s.pH.WithLabelValues("FlatRecords", "complete", "count").Observe(time.Since(startTime).Seconds())
		s.pC.WithLabelValues("FlatRecords", "complete", "count").Inc()
	}()
	s.pC.WithLabelValues("FlatRecords", "start", "count").Inc()

	ctx := stream.Context()

	s.FlatRecordsClients.Store(&stream, true)
	s.frStoreCount.Add(1)
	defer func() {
		s.FlatRecordsClients.Delete(&stream)
		s.frDeleteCount.Add(1)
	}()

	// xtcpFlatRecordsResponse := s.FlatRecordsResponsePool.Get().(*xtcp_flat_record.FlatRecordsResponse)
	// defer s.FlatRecordsResponsePool.Put(xtcpFlatRecordsResponse)

	select {
	case <-s.ctx.Done():
	case <-ctx.Done():
		//default:
		// Block
	}

	return nil
}

func (s *xtcpFlatRecordService) PollFlatRecords(
	stream grpc.BidiStreamingServer[xtcp_flat_record.PollFlatRecordsRequest, xtcp_flat_record.PollFlatRecordsResponse]) error {

	startTime := time.Now()
	defer func() {
		s.pH.WithLabelValues("PollFlatRecords", "complete", "count").Observe(time.Since(startTime).Seconds())
		s.pC.WithLabelValues("PollFlatRecords", "complete", "count").Inc()
	}()
	s.pC.WithLabelValues("PollFlatRecords", "start", "count").Inc()

	ctx := stream.Context()

	s.PollFlatRecordsClients.Store(&stream, true)
	s.pfrStoreCount.Add(1)
	defer func() {
		s.PollFlatRecordsClients.Delete(&stream)
		s.pfrDeleteCount.Add(1)
	}()

	for {
		select {
		case <-s.ctx.Done():
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// receive data from stream
		_, err := stream.Recv()
		s.pC.WithLabelValues("PollFlatRecords", "srv.Recv", "count").Inc()
		if err == io.EOF {
			s.pC.WithLabelValues("PollFlatRecords", "io.EOF", "count").Inc()
			if s.debugLevel > 10 {
				log.Println("PollFlatRecords io.EOF")
			}
			return nil
		}

		if err != nil {
			s.pC.WithLabelValues("PollFlatRecords", "srv.Recv", "error").Inc()
			if s.debugLevel > 10 {
				log.Printf("PollFlatRecords receive error %v", err)
			}
			continue
		}
		*s.pollRequestCh <- struct{}{}
		if s.debugLevel > 10 {
			log.Printf("PollFlatRecords *s.pollRequestCh <- struct{}{}")
		}
	}

}

// frMapCount is a small helper to determine the number
// of items in the FlatRecordsClients map
func (s *xtcpFlatRecordService) frMapCount() (count uint64) {
	store := s.frStoreCount.Load()
	delete := s.frDeleteCount.Load()
	count = store - delete
	if s.debugLevel > 1000 {
		log.Printf("frMapCount:%d", count)
	}
	return count
}

// pfrMapCount is a small helper to determine the number
// of items in the FlatRecordsClients map
func (s *xtcpFlatRecordService) pfrMapCount() (count uint64) {
	store := s.pfrStoreCount.Load()
	delete := s.pfrDeleteCount.Load()
	count = store - delete
	if s.debugLevel > 1000 {
		log.Printf("pfrMapCount:%d", count)
	}
	return count
}

// flatRecordServiceSend is called with a protobuf record, and does the grpc .Send()
// for each connected GRPC client
func (x *XTCP) flatRecordServiceSend(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {

	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("flatRecordServiceSend", "complete", "count").Observe(time.Since(startTime).Seconds())
		x.pC.WithLabelValues("flatRecordServiceSend", "complete", "count").Inc()
	}()
	x.pC.WithLabelValues("flatRecordServiceSend", "start", "count").Inc()

	// Check if there are clients
	frClientCount := x.flatRecordService.frMapCount()
	pfrClientCount := x.flatRecordService.pfrMapCount()

	if x.debugLevel > 1000 {
		log.Printf("flatRecordServiceSend frClientCount:%d pfrClientCount:%d", frClientCount, pfrClientCount)
	}

	if frClientCount == 0 && pfrClientCount == 0 {
		if x.debugLevel > 1000 {
			log.Printf("flatRecordServiceSend no clients, frClientCount:%d pfrClientCount:%d", frClientCount, pfrClientCount)
		}
		return
	}

	xtcpFlatRecordsResponse := x.flatRecordService.FlatRecordsResponsePool.Get().(*xtcp_flat_record.FlatRecordsResponse)
	//defer x.flatRecordService.FlatRecordsResponsePool.Put(xtcpFlatRecordsResponse)

	(*xtcpFlatRecordsResponse).XtcpFlatRecord = xtcpRecord

	if frClientCount > 0 {
		x.flatRecordService.FlatRecordsClients.Range(func(k, v interface{}) bool {

			stream := k.(*xtcp_flat_record.XTCPFlatRecordService_FlatRecordsServer)
			if err := (*stream).Send(xtcpFlatRecordsResponse); err != nil { // <<------------------------- Send
				x.pC.WithLabelValues("flatRecordServiceSend", "frSend", "error").Inc()
			}
			x.pC.WithLabelValues("flatRecordServiceSend", "frSent", "count").Inc()
			if x.debugLevel > 1000 {
				log.Printf("flatRecordServiceSend frSend")
			}

			return true
		})
	}

	if pfrClientCount > 0 {
		x.flatRecordService.PollFlatRecordsClients.Range(func(k, v interface{}) bool {

			stream := k.(*grpc.BidiStreamingServer[xtcp_flat_record.PollFlatRecordsRequest, xtcp_flat_record.FlatRecordsResponse])
			if err := (*stream).Send(xtcpFlatRecordsResponse); err != nil { // <<------------------------- Send
				x.pC.WithLabelValues("flatRecordServiceSend", "pfrSend", "error").Inc()
			}
			x.pC.WithLabelValues("flatRecordServiceSend", "pfrSent", "count").Inc()
			if x.debugLevel > 1000 {
				log.Printf("flatRecordServiceSend pfrSend")
			}

			return true
		})
	}

	//xtcpFlatRecordsResponse.Reset()
	x.flatRecordService.FlatRecordsResponsePool.Put(xtcpFlatRecordsResponse)

}
