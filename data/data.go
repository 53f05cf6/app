package data

type Data interface {
	Get() error
	Csv() string
}
