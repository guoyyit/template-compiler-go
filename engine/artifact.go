package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"io"
)

const (
	artifactMagic   = "TCG1"
	artifactVersion = 1
)

// Artifact 是可序列化的编译产物。
type Artifact struct {
	FormatVersion int
	Name          string
	SourceHash    string
	Instr         []Instr
	Deps          []string
}

// encodeArtifact 把产物写入 w。
// 文件布局：magic(4) | version(1) | payloadSHA256(32) | gobPayload
func encodeArtifact(w io.Writer, a *Artifact) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(a); err != nil {
		return err
	}
	payload := buf.Bytes()
	sum := sha256.Sum256(payload)
	if _, err := w.Write([]byte(artifactMagic)); err != nil {
		return err
	}
	if _, err := w.Write([]byte{artifactVersion}); err != nil {
		return err
	}
	if _, err := w.Write(sum[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// decodeArtifact 从 r 读取并校验产物。
// 魔数/版本不兼容返回 ErrArtifactIncompatible；
// 校验和失败或 gob 损坏返回 ErrArtifactCorrupt。
func decodeArtifact(r io.Reader) (*Artifact, error) {
	header := make([]byte, 4+1+32)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactCorrupt, err)
	}
	if string(header[0:4]) != artifactMagic {
		return nil, ErrArtifactIncompatible
	}
	version := int(header[4])
	if version != artifactVersion {
		return nil, fmt.Errorf("%w: got version %d, want %d", ErrArtifactIncompatible, version, artifactVersion)
	}
	var wantSum [32]byte
	copy(wantSum[:], header[5:37])

	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactCorrupt, err)
	}
	gotSum := sha256.Sum256(payload)
	if gotSum != wantSum {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrArtifactCorrupt)
	}

	var a Artifact
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&a); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactCorrupt, err)
	}
	if err := validateArtifact(&a); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactCorrupt, err)
	}
	return &a, nil
}

// validateArtifact 做基本的健全性检查，防止损坏数据导致渲染崩溃。
func validateArtifact(a *Artifact) error {
	if a.FormatVersion != artifactVersion {
		return fmt.Errorf("format version %d", a.FormatVersion)
	}
	if a.Instr == nil {
		return fmt.Errorf("missing instructions")
	}
	for i, in := range a.Instr {
		switch in.Op {
		case opText, opWrite, opJumpIfFalse, opJump, opForInit, opForNext, opInclude:
		default:
			return fmt.Errorf("instr %d: unknown opcode %d", i, in.Op)
		}
		switch in.Op {
		case opJumpIfFalse, opJump, opForNext:
			if in.Target < 0 || in.Target > len(a.Instr) {
				return fmt.Errorf("instr %d: jump target %d out of range", i, in.Target)
			}
		}
		if in.Op == opWrite || in.Op == opJumpIfFalse || in.Op == opForInit {
			if in.Expr == nil {
				return fmt.Errorf("instr %d: missing expression", i)
			}
		}
	}
	return nil
}
