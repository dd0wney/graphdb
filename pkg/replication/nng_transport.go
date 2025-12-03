//go:build nng
// +build nng

package replication

import (
	"time"

	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/pub"
	"go.nanomsg.org/mangos/v3/protocol/pull"
	"go.nanomsg.org/mangos/v3/protocol/push"
	"go.nanomsg.org/mangos/v3/protocol/respondent"
	"go.nanomsg.org/mangos/v3/protocol/sub"
	"go.nanomsg.org/mangos/v3/protocol/surveyor"

	// Register all transports
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

// nngSocket wraps a mangos.Socket to implement our Socket interface.
type nngSocket struct {
	sock mangos.Socket
}

func (s *nngSocket) Send(data []byte) error {
	return s.sock.Send(data)
}

func (s *nngSocket) Recv() ([]byte, error) {
	return s.sock.Recv()
}

func (s *nngSocket) Close() error {
	return s.sock.Close()
}

func (s *nngSocket) SetRecvDeadline(d time.Duration) error {
	return s.sock.SetOption(mangos.OptionRecvDeadline, d)
}

func (s *nngSocket) SetSendDeadline(d time.Duration) error {
	return s.sock.SetOption(mangos.OptionSendDeadline, d)
}

func (s *nngSocket) Listen(addr string) error {
	return s.sock.Listen(addr)
}

func (s *nngSocket) Dial(addr string) error {
	return s.sock.Dial(addr)
}

// nngSubSocket adds subscription capability.
type nngSubSocket struct {
	nngSocket
}

func (s *nngSubSocket) Subscribe(topic []byte) error {
	return s.sock.SetOption(mangos.OptionSubscribe, topic)
}

// nngSurveySocket adds survey time configuration.
type nngSurveySocket struct {
	nngSocket
}

func (s *nngSurveySocket) SetSurveyTime(d time.Duration) error {
	return s.sock.SetOption(mangos.OptionSurveyTime, d)
}

// NNGSocketFactory creates NNG/mangos sockets.
type NNGSocketFactory struct{}

// NewNNGSocketFactory creates a new NNG socket factory.
func NewNNGSocketFactory() *NNGSocketFactory {
	return &NNGSocketFactory{}
}

func (f *NNGSocketFactory) NewPubSocket() (ListenSocket, error) {
	sock, err := pub.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSocket{sock: sock}, nil
}

func (f *NNGSocketFactory) NewSubSocket() (SubscribeSocket, error) {
	sock, err := sub.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSubSocket{nngSocket{sock: sock}}, nil
}

func (f *NNGSocketFactory) NewSurveyorSocket() (SurveySocket, error) {
	sock, err := surveyor.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSurveySocket{nngSocket{sock: sock}}, nil
}

func (f *NNGSocketFactory) NewRespondentSocket() (DialSocket, error) {
	sock, err := respondent.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSocket{sock: sock}, nil
}

func (f *NNGSocketFactory) NewPushSocket() (DialSocket, error) {
	sock, err := push.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSocket{sock: sock}, nil
}

func (f *NNGSocketFactory) NewPullSocket() (ListenSocket, error) {
	sock, err := pull.NewSocket()
	if err != nil {
		return nil, err
	}
	return &nngSocket{sock: sock}, nil
}

// Ensure NNGSocketFactory implements SocketFactory
var _ SocketFactory = (*NNGSocketFactory)(nil)
