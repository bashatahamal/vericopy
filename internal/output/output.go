package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Envelope is the stable top-level JSON contract.
type Envelope struct {
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *verrors.Error `json:"error,omitempty"`
}

// Printer writes human or JSON output. Progress never goes through JSON mode.
type Printer struct {
	Out   io.Writer
	Err   io.Writer
	JSON  bool
	Quiet bool
}

func (p Printer) Success(human string, result any) error {
	if p.JSON {
		return json.NewEncoder(p.Out).Encode(Envelope{OK: true, Result: result})
	}
	if p.Quiet || human == "" {
		return nil
	}
	_, err := fmt.Fprintln(p.Out, human)
	return err
}

func (p Printer) Failure(err error) error {
	diagnostic := verrors.As(err)
	if p.JSON {
		return json.NewEncoder(p.Out).Encode(Envelope{OK: false, Error: diagnostic})
	}
	if _, writeErr := fmt.Fprintf(p.Err, "%s\n\n%s\n", diagnostic.Code, diagnostic.Message); writeErr != nil {
		return writeErr
	}
	if diagnostic.Hint != "" {
		_, writeErr := fmt.Fprintf(p.Err, "\nNext: %s\n", diagnostic.Hint)
		return writeErr
	}
	return nil
}
