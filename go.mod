module github.com/gardener/gardener

go 1.13

require (
	github.com/Masterminds/semver v1.4.2
	github.com/Masterminds/sprig v2.22.0+incompatible // indirect
	github.com/ahmetb/gen-crd-api-reference-docs v0.1.5
	github.com/elazarl/goproxy v0.0.0-20191011121108-aa519ddbe484 // indirect
	github.com/frankban/quicktest v1.5.0 // indirect
	github.com/gardener/controller-manager-library v0.0.0-20191022090355-2f744b5822cc // indirect
	github.com/gardener/external-dns-management v0.0.0-20190220100540-b4bbb5832a03
	github.com/gardener/gardener-extensions v0.0.0-20191028142629-438a3dcf5eca
	github.com/gardener/gardener-resource-manager v0.8.1
	github.com/gardener/hvpa-controller v0.0.0-20191014062307-fad3bdf06a25
	github.com/gardener/machine-controller-manager v0.0.0-20191118095523-e30355bc7945
	github.com/ghodss/yaml v1.0.0
	github.com/go-openapi/spec v0.19.2
	github.com/golang/mock v1.3.1
	github.com/googleapis/gnostic v0.3.0
	github.com/grpc-ecosystem/grpc-gateway v1.11.3 // indirect
	github.com/hashicorp/go-multierror v0.0.0-20180717150148-3d5d8f294aa0
	github.com/json-iterator/go v1.1.6
	github.com/mholt/archiver v3.1.1+incompatible
	github.com/mitchellh/copystructure v1.0.0 // indirect
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/pierrec/lz4 v2.3.0+incompatible // indirect
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.1.0
	github.com/prometheus/common v0.6.0
	github.com/robfig/cron v1.2.0
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4
	golang.org/x/exp v0.0.0-20191126135315-41df83031236 // indirect
	golang.org/x/lint v0.0.0-20190409202823-959b441ac422
	golang.org/x/tools v0.0.0-20191126055441-b0650ceb63d9 // indirect
	gonum.org/v1/gonum v0.6.1
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20191004102349-159aefb8556b
	k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery v0.0.0-20191004074956-c5d2f014d689
	k8s.io/apiserver v0.0.0-20191010014313-3893be10d307
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cluster-bootstrap v0.0.0-20190816225014-88e17f53ad9d
	k8s.io/code-generator v0.0.0-20190713022532-93d7507fc8ff
	k8s.io/component-base v0.0.0-20190816222507-f3799749b6b7
	k8s.io/helm v2.14.2+incompatible
	k8s.io/klog v0.3.3
	k8s.io/kube-aggregator v0.0.0-20191004104030-d9d5f0cc7532
	k8s.io/kube-openapi v0.0.0-20190722073852-5e22f3d471e6
	k8s.io/metrics v0.0.0-20191004105854-2e8cf7d0888c
	k8s.io/utils v0.0.0-20190607212802-c55fbcfc754a
	sigs.k8s.io/controller-runtime v0.2.0-beta.5
)

replace (
	github.com/gardener/gardener-extensions => github.com/gardener/gardener-extensions v0.0.0-20191028142629-438a3dcf5eca
	github.com/gardener/machine-controller-manager => github.com/gardener/machine-controller-manager v0.0.0-20191118095523-e30355bc7945
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.0.0-20191004102349-159aefb8556b // kubernetes-1.14.8
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed // kubernetes-1.14.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20191004074956-c5d2f014d689 // kubernetes-1.14.8
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20191010014313-3893be10d307 // kubernetes-1.14.8
	k8s.io/client-go => k8s.io/client-go v11.0.0+incompatible // kubernetes-1.14.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20190816225014-88e17f53ad9d // kubernetes-1.14.8
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190704094409-6c2a4329ac29 // kubernetes-1.14.8
	k8s.io/component-base => k8s.io/component-base v0.0.0-20190816222507-f3799749b6b7 // kubernetes-1.14.8
	k8s.io/helm => k8s.io/helm v2.13.1+incompatible
	k8s.io/klog => k8s.io/klog v0.1.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20191004104030-d9d5f0cc7532 // kubernetes-1.14.8
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20190320154901-5e45bb682580
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.2.0-beta.5
	sigs.k8s.io/structured-merge-diff => sigs.k8s.io/structured-merge-diff v0.0.0-20190302045857-e85c7b244fd2
)
