package rabbitmq

import "testing"

func TestRetryTopologyValidate(t *testing.T) {
	valid := RetryTopology{
		QueueName:     "notification.otp",
		RoutingKey:    "notification.otp",
		RetryExchange: "notification.otp.retry",
		DLQExchange:   "notification.otp.dlq",
		DLQQueueName:  "notification.otp.dlq",
	}

	cases := []struct {
		name    string
		mutate  func(*RetryTopology)
		wantErr bool
	}{
		{name: "valid", wantErr: false},
		{name: "missing queue", mutate: func(t *RetryTopology) { t.QueueName = "" }, wantErr: true},
		{name: "missing routing key", mutate: func(t *RetryTopology) { t.RoutingKey = "" }, wantErr: true},
		{name: "missing retry exchange", mutate: func(t *RetryTopology) { t.RetryExchange = "" }, wantErr: true},
		{name: "missing dlq exchange", mutate: func(t *RetryTopology) { t.DLQExchange = "" }, wantErr: true},
		{name: "missing dlq queue", mutate: func(t *RetryTopology) { t.DLQQueueName = "" }, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topology := valid
			if tc.mutate != nil {
				tc.mutate(&topology)
			}

			err := topology.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultRetryTopologyNames(t *testing.T) {
	queueName := "push.dispatch"

	if got := DefaultRetryExchangeName(queueName); got != "push.dispatch.retry" {
		t.Fatalf("DefaultRetryExchangeName = %q", got)
	}
	if got := DefaultDLQExchangeName(queueName); got != "push.dispatch.dlq" {
		t.Fatalf("DefaultDLQExchangeName = %q", got)
	}
	if got := DefaultDLQQueueName(queueName); got != "push.dispatch.dlq" {
		t.Fatalf("DefaultDLQQueueName = %q", got)
	}
}
