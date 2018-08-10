package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

var (
	errCertPath       = errors.New("DB_CA_CERT_PATH is required and was not set")
	errClientCertPath = errors.New("DB_CLIENT_CERT_PATH is required and was not set")
	errClientKeyPath  = errors.New("DB_CLIENT_KEY_PATH is required and was not set")
	errCertPEM        = errors.New("trusted conn with DB not established, cannot parse cert PEM")
)

// TODO: add TLS listener with default :8443

func main() {
	log.SetFlags(log.Llongfile | log.LUTC | log.LstdFlags)
	log.SetPrefix("reqlogd: ")
	log.Println("starting")

	addr := ":8080"
	if v, ok := os.LookupEnv("REQLOGD_ADDR"); ok {
		addr = v
	}

	dconf, err := dbConfFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	d, err := newDB(dconf)
	if err != nil {
		log.Fatal(err)
	}

	s := server{db: d, now: time.Now}
	http.HandleFunc("/", s.serveReqLog)

	log.Printf("listening on %v", addr)
	http.ListenAndServe(addr, nil)
}

type server struct {
	db interface {
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
	now func() time.Time
}

func dbConfFromEnv() (*mysql.Config, error) {
	dconf := &mysql.Config{
		User:      "root",
		Passwd:    "",
		Net:       "tcp",
		Addr:      "localhost:3306",
		DBName:    "test",
		Loc:       time.UTC,
		ParseTime: true,
	}
	if v, ok := os.LookupEnv("DB_USER"); ok {
		dconf.User = v
	}
	if v, ok := os.LookupEnv("DB_PASS"); ok {
		dconf.Passwd = v
	}
	if v, ok := os.LookupEnv("DB_ADDR"); ok {
		dconf.Addr = v
	}
	if v, ok := os.LookupEnv("DB_NAME"); ok {
		dconf.DBName = v
	}
	if _, ok := os.LookupEnv("DB_SKIP_TLS"); ok {
		return dconf, nil
	}
	caCertPath, ok := os.LookupEnv("DB_CA_CERT_PATH")
	if !ok {
		return nil, errCertPath
	}
	clientCertPath, ok := os.LookupEnv("DB_CLIENT_CERT_PATH")
	if !ok {
		return nil, errClientCertPath
	}
	clientKeyPath, ok := os.LookupEnv("DB_CLIENT_KEY_PATH")
	if !ok {
		return nil, errClientKeyPath
	}
	tconf, err := tlsConfig(caCertPath, clientCertPath, clientKeyPath)
	if err != nil {
		return nil, err
	}
	dconf.TLSConfig = tconf
	return dconf, nil
}

// tlsConfig calls mysql driver to enable TLS for mysql connection. tconfKey is
// a key to retrieve the specific tls.Config created by tlsConfig.  It should
// be used in the db connection string as the value of the tls param.  Use
// caCertPath to specify the trusted certificates for the database. Use
// clientCertPath and clientKeyPath to specify the client certificate and key
// to be used for the db connection.
func tlsConfig(caCertPath, clientCertPath, clientKeyPath string) (tconfKey string, err error) {
	pem, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		return "", err
	}
	rootCertPool := x509.NewCertPool()
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return "", errCertPEM
	}
	cert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return "", err
	}
	dbTLSConfig := &tls.Config{
		RootCAs:      rootCertPool,
		Certificates: []tls.Certificate{cert},
	}
	tconfKey = "custom"
	if err := mysql.RegisterTLSConfig(tconfKey, dbTLSConfig); err != nil {
		return "", err
	}
	return tconfKey, nil
}

type db struct {
	db *sql.DB
}

func newDB(dconf *mysql.Config) (*db, error) {
	d, err := sql.Open("mysql", dconf.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("newDB Open: %v", err)
	}
	if err := d.Ping(); err != nil {
		return nil, fmt.Errorf("newDB Ping: %v", err)
	}
	return &db{db: d}, nil
}

func (d *db) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return d.db.ExecContext(ctx, query, args...)
}

/*
CREATE TABLE `test`.`request` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `rat` DATETIME NOT NULL,
  `url` TEXT NOT NULL,
  `method` TEXT(16) NOT NULL,
  `remote` TEXT(64) NOT NULL,
  `headers` MEDIUMTEXT NULL,
  `length` INT NOT NULL,
  `protocol` TEXT(16) NOT NULL,
  PRIMARY KEY (`id`));
*/

func (s *server) serveReqLog(w http.ResponseWriter, r *http.Request) {
	recievedAt := s.now().UTC()
	var u url.URL
	u = *r.URL
	u.Host = r.Host
	u.Scheme = "http"
	if r.TLS != nil {
		u.Scheme = "https"
	}
	url := u.String()
	method := r.Method
	remote := r.RemoteAddr
	headers := fmt.Sprint(r.Header)
	length := 0
	if r.Body != nil {
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Printf("serveReqLog: got error reading request body: %v", err)
		}
		length = len(b)
	}
	protocol := r.Proto

	_, err := s.db.ExecContext(r.Context(),
		`insert into request (rat, url, method, remote, headers, length, protocol)
		values (?, ?, ?, ?, ?, ?, ?)`,
		recievedAt, url, method, remote, headers, length, protocol,
	)
	if err != nil {
		log.Printf("serveReqLog: got error writing to DB: %v", err)
		jsonError(w, fmt.Sprintf("internal error: %v", err), 500)
		return
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	if err := json.NewEncoder(w).Encode(struct{ msg string }{msg: msg}); err != nil {
		log.Printf("jsonError: got error writing to client: %v", err)
	}
	w.WriteHeader(code)
}
