package admission_webhook

import (
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func NewHookServer(certDir string, port int) webhook.Server {
	s := webhook.NewServer(webhook.Options{
		Port:     port,
		CertDir:  certDir,
		CertName: `serverCert.pem`,
		KeyName:  `serverKey.pem`,
	})

	s.Register(`/mutate/set-default-value`, &webhook.Admission{Handler: NewMutateHandler()})
	return s
}
