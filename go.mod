module github.com/dollarshaveclub/furan

go 1.14

replace github.com/docker/docker => github.com/docker/engine v17.12.0-ce-rc1.0.20190717161051-705d9623b7c1+incompatible

replace github.com/Sirupsen/logrus v1.4.2 => github.com/sirupsen/logrus v1.4.2

require (
	github.com/DataDog/datadog-go v3.7.1+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/docker v1.13.1
	github.com/dollarshaveclub/pvc v0.0.0-20190128210719-5ec81720bc8a
	github.com/frankban/quicktest v1.7.2 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/golang-migrate/migrate/v4 v4.11.0
	github.com/golang/mock v1.4.3 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/google/go-cmp v0.4.1 // indirect
	github.com/google/go-github v17.0.0+incompatible
	github.com/gorilla/mux v1.7.4
	github.com/gotestyourself/gotestyourself v1.4.0 // indirect
	github.com/hashicorp/consul/api v1.4.0
	github.com/hashicorp/go-retryablehttp v0.6.6 // indirect
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/hashicorp/vault/api v1.0.4 // indirect
	github.com/jackc/pgconn v1.5.0
	github.com/jackc/pgtype v1.3.0
	github.com/jackc/pgx/v4 v4.6.0
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pierrec/lz4 v2.4.1+incompatible // indirect
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v1.0.0
	github.com/tinylib/msgp v1.1.2 // indirect
	golang.org/x/crypto v0.0.0-20200604202706-70a84ac30bf9 // indirect
	golang.org/x/net v0.0.0-20200602114024-627f9648deb9
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a // indirect
	golang.org/x/sys v0.0.0-20200610111108-226ff32320da // indirect
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1 // indirect
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/genproto v0.0.0-20200610104632-a5b850bcf112 // indirect
	google.golang.org/grpc v1.29.1
	gopkg.in/DataDog/dd-trace-go.v1 v1.24.1
	gopkg.in/yaml.v2 v2.3.0 // indirect
	gotest.tools v1.4.0 // indirect
)
