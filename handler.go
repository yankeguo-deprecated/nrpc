package nrpc

import (
	"context"
	"encoding/json"
	"go.guoyk.net/trackid"
	"net/http"
	"reflect"
	"strconv"
)

var (
	typeContext = reflect.TypeOf((*context.Context)(nil)).Elem()
	typeError   = reflect.TypeOf((*error)(nil)).Elem()
)

const (
	HeaderCorrelationID = "X-Correlation-Id"
)

type Handler struct {
	svc string
	mtd string
	tgt interface{}
	fn  reflect.Value
	in  reflect.Type
}

func checkRPCFunc(t reflect.Type) (in reflect.Type, ok bool) {
	if t.NumIn() == 2 {
		if !typeContext.AssignableTo(t.In(1)) {
			return
		}
	} else if t.NumIn() == 3 {
		if !typeContext.AssignableTo(t.In(1)) {
			return
		}
		t1 := t.In(2)
		if t1.Kind() != reflect.Ptr {
			return
		}
		if t1.Elem().Kind() != reflect.Struct {
			return
		}
		in = t1.Elem()
	} else {
		return
	}
	if t.NumOut() == 1 {
		if !t.Out(0).AssignableTo(typeError) {
			return
		}
	} else if t.NumOut() == 2 {
		if t.Out(0).Kind() != reflect.Struct {
			return
		}
		if !t.Out(1).AssignableTo(typeError) {
			return
		}
	} else {
		return
	}
	ok = true
	return
}

// ExtractHandlers create a Map of *Handler based on receiver's methods
// supported signatures:
//  - Method1(ctx context.Context) (err error)
//  - Method2(ctx context.Context, in *SomeStruct1) (err error)
//  - Method3(ctx context.Context, in *SomeStruct1) (out SomeStruct2, err error)
//  - Method4(ctx context.Context) (out SomeStruct2, err error)
func ExtractHandlers(name string, tgt interface{}) map[string]*Handler {
	ret := map[string]*Handler{}
	t := reflect.TypeOf(tgt)
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if in, ok := checkRPCFunc(m.Type); ok {
			ret[m.Name] = &Handler{svc: name, mtd: m.Name, tgt: tgt, fn: m.Func, in: in}
		}
	}
	return ret
}

func sendError(rw http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if IsUserError(err) {
		code = http.StatusBadRequest
	}
	buf := []byte(err.Error())
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	rw.WriteHeader(code)
	_, _ = rw.Write(buf)
}

func sendBody(rw http.ResponseWriter, body interface{}) {
	if body == nil {
		rw.WriteHeader(http.StatusOK)
	} else {
		if buf, err := json.Marshal(body); err != nil {
			sendError(rw, err)
		} else {
			rw.Header().Set("Content-Type", "application/json; charset=utf-8")
			rw.Header().Set("Content-Length", strconv.Itoa(len(buf)))
			_, _ = rw.Write(buf)
		}
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// setup correlation id
	ctx := trackid.Set(req.Context(), req.Header.Get(HeaderCorrelationID))
	rw.Header().Set(HeaderCorrelationID, trackid.Get(ctx))

	// build args
	args := []reflect.Value{reflect.ValueOf(h.tgt), reflect.ValueOf(ctx)}
	if h.in != nil {
		v := reflect.New(h.in).Interface()
		dec := json.NewDecoder(req.Body)
		if err := dec.Decode(v); err != nil {
			sendError(rw, err)
			return
		}
		args = append(args, reflect.ValueOf(v))
	}

	// call
	rets := h.fn.Call(args)

	// build response
	var err error
	var out interface{}
	if len(rets) == 1 {
		if !rets[0].IsNil() {
			err = rets[0].Interface().(error)
		}
	} else {
		out = rets[0].Interface()
		if !rets[1].IsNil() {
			err = rets[1].Interface().(error)
		}
	}
	if err != nil {
		sendError(rw, err)
	} else {
		sendBody(rw, out)
	}
}
