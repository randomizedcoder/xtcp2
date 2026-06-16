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
	"github.com/randomizedcoder/xtcp2/pkg/xsync"
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

	FlatRecordsResponsePool *xsync.Pool[*xtcp_flat_record.FlatRecordsResponse]

	pollRequestCh *chan struct{}

	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec

	debugLevel uint32
}

// NewXtcpFlatRecordService builds the gRPC FlatRecordService. The reg
// argument is the prometheus.Registerer the service's CounterVec +
// SummaryVec are registered into; pass nil to use
// prometheus.DefaultRegisterer (the production default). Tests inject a
// fresh prometheus.NewRegistry() so the constructor can be called more
// than once per process.
func NewXtcpFlatRecordService(ctx context.Context, reg prometheus.Registerer, pollRequestCh *chan struct{}, debugLevel uint32) *xtcpFlatRecordService {

	s := new(xtcpFlatRecordService)

	s.debugLevel = debugLevel
	s.ctx = ctx

	s.FlatRecordsResponsePool = xsync.NewPool(func() *xtcp_flat_record.FlatRecordsResponse {
		return new(xtcp_flat_record.FlatRecordsResponse)
	})

	s.pollRequestCh = pollRequestCh

	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)

	s.pC = factory.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp_record_grpc",
			Name:      promNameCounts,
			Help:      promHelpCounts,
		},
		promLabels,
	)

	s.pH = factory.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_record_grpc",
			Name:      promNameHistograms,
			Help:      promHelpHistograms,
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		promLabels,
	)

	return s
}

// FlatRecords is the GRPC handler.  This accepts the client,
// stores the pointer to the client's "stream" in the map, which allows
// other goroutines to send data on the stream.
// Then this goroutine just blocks waiting for the client to disconnect,
// or the context to be canceled()
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
		// Buffered channel of size 2 — third in-flight poke would block
		// forever if the poller isn't draining (mid-shutdown, paused,
		// already-polling state). Non-blocking send so the RPC handler
		// stays responsive; observe both the stream ctx and the
		// service-level ctx so coalesced pokes don't wedge teardown.
		select {
		case *s.pollRequestCh <- struct{}{}:
		case <-ctx.Done():
			s.pC.WithLabelValues("PollFlatRecords", "ctxDone", "count").Inc()
			return ctx.Err()
		case <-s.ctx.Done():
			s.pC.WithLabelValues("PollFlatRecords", "serverCtxDone", "count").Inc()
			return s.ctx.Err()
		default:
			s.pC.WithLabelValues("PollFlatRecords", "chFull", "count").Inc()
		}
		if s.debugLevel > 10 {
			log.Printf("PollFlatRecords *s.pollRequestCh <- struct{}{}")
		}
	}

}

// frMapCount is a small helper to determine the number
// of items in the FlatRecordsClients map
func (s *xtcpFlatRecordService) frMapCount() (count uint64) {
	store := s.frStoreCount.Load()
	deleted := s.frDeleteCount.Load()
	count = store - deleted
	if s.debugLevel > 1000 {
		log.Printf("frMapCount:%d", count)
	}
	return count
}

// pfrMapCount is a small helper to determine the number
// of items in the FlatRecordsClients map
func (s *xtcpFlatRecordService) pfrMapCount() (count uint64) {
	store := s.pfrStoreCount.Load()
	deleted := s.pfrDeleteCount.Load()
	count = store - deleted
	if s.debugLevel > 1000 {
		log.Printf("pfrMapCount:%d", count)
	}
	return count
}

// flatRecordServiceSend is called with a protobuf record, and does the grpc .Send()
// for each connected GRPC client
func (x *XTCP) flatRecordServiceSend(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
	// func (x *XTCP) flatRecordServiceSend(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {

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

	xtcpFlatRecordsResponse := x.flatRecordService.FlatRecordsResponsePool.Get()
	// Reset the pooled response so its internal proto state (state,
	// sizeCache, unknownFields) is cleared from the previous send. The
	// per-record XtcpFlatRecord pointer assignment that follows is the
	// only meaningful user-visible field, but proto.Marshal trusts
	// sizeCache for the on-wire byte count; reusing a recycled response
	// without Reset can mis-encode the next message if its protobuf
	// size differs from the previous one. Same shape as bug 55 (the
	// kgo.Record fix) — partially-overwriting a recycled struct is the
	// pattern.
	xtcpFlatRecordsResponse.Reset()

	xtcpFlatRecordsResponse.XtcpFlatRecord = xtcpRecord

	if frClientCount > 0 {
		x.flatRecordService.FlatRecordsClients.Range(func(k, v interface{}) bool {

			stream, ok := k.(*xtcp_flat_record.XTCPFlatRecordService_FlatRecordsServer)
			if !ok {
				return true
			}
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
		// PollFlatRecords stores its streams as the bidi server type whose
		// SECOND type param is PollFlatRecordsResponse (not FlatRecordsResponse
		// — that's what the regular FlatRecords stream takes). Asserting on
		// the wrong type produced nil + a nil-deref panic on send; nothing
		// caught it earlier because no test or production run had ever held
		// a pfr stream open AND fired flatRecordServiceSend at the same time.
		// PollFlatRecordsResponse.XtcpFlatRecord mirrors FlatRecordsResponse,
		// so we reuse the xtcpRecord pointer and wrap it.
		pollResp := &xtcp_flat_record.PollFlatRecordsResponse{
			XtcpFlatRecord: xtcpRecord,
		}
		x.flatRecordService.PollFlatRecordsClients.Range(func(k, v interface{}) bool {

			stream, ok := k.(*grpc.BidiStreamingServer[xtcp_flat_record.PollFlatRecordsRequest, xtcp_flat_record.PollFlatRecordsResponse])
			if !ok {
				return true
			}
			if err := (*stream).Send(pollResp); err != nil { // <<------------------------- Send
				x.pC.WithLabelValues("flatRecordServiceSend", "pfrSend", "error").Inc()
			}
			x.pC.WithLabelValues("flatRecordServiceSend", "pfrSent", "count").Inc()
			if x.debugLevel > 1000 {
				log.Printf("flatRecordServiceSend pfrSend")
			}

			return true
		})
	}

	x.flatRecordService.FlatRecordsResponsePool.Put(xtcpFlatRecordsResponse)

}
