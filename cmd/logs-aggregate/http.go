package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func httpStream(addr string, follow bool) io.ReadCloser {
	r, w := io.Pipe()
	srv := &http.Server{
		Addr: addr,
	}

	close := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := srv.Shutdown(ctx)
		if err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}

	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, fmt.Sprintf("method %q not allowed, use %q", req.Method, http.MethodPost), http.StatusMethodNotAllowed)
			return
		}
		defer req.Body.Close()
		_, err := io.Copy(w, req.Body)
		if err == io.EOF && follow {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if err == io.EOF {
			err = close()
			if err != nil {
				_, _ = w.Write([]byte("Error: " + err.Error()))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		http.Error(w, fmt.Sprintf("unexpected error: %v", err), http.StatusInternalServerError)
	})

	go func() {
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			return
		}
		if err != nil {
			_, _ = w.Write([]byte("Error: " + err.Error()))
		}
	}()

	rClose := func() error {
		err := close()
		if err != nil {
			r.Close()
			return err
		}
		return r.Close()
	}

	return readCloser{
		Reader:  r,
		closeFn: rClose,
	}
}

type readCloser struct {
	io.Reader
	closeFn func() error
}

func (rc readCloser) Close() error {
	return rc.closeFn()
}
