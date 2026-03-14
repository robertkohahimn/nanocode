package engine

import (
	"strings"
	"testing"
)

func TestCheckpointInjectorGentle(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(10)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 10")
	}
	if level != "gentle" {
		t.Errorf("expected gentle level, got %q", level)
	}
	if msg.Type != "text" {
		t.Errorf("expected text block, got %q", msg.Type)
	}
	if !strings.Contains(msg.Text, "Checkpoint") {
		t.Error("expected checkpoint text")
	}
	if !strings.Contains(msg.Text, "accomplished") {
		t.Error("expected gentle tone at iteration 10")
	}
}

func TestCheckpointInjectorWarning(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(30)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 30")
	}
	if level != "warning" {
		t.Errorf("expected warning level, got %q", level)
	}
	if !strings.Contains(msg.Text, "halfway") || !strings.Contains(msg.Text, "stuck") {
		t.Error("expected warning tone at iteration 30")
	}
}

func TestCheckpointInjectorUrgent(t *testing.T) {
	ci := NewCheckpointInjector(10)
	msg, level := ci.MaybeInject(40)
	if msg == nil {
		t.Fatal("expected checkpoint at iteration 40")
	}
	if level != "urgent" {
		t.Errorf("expected urgent level, got %q", level)
	}
	if !strings.Contains(msg.Text, "stopping") {
		t.Error("expected urgent tone at iteration 40")
	}
}

func TestCheckpointInjectorNoFireBetween(t *testing.T) {
	ci := NewCheckpointInjector(10)
	for _, iter := range []int{0, 1, 5, 9, 11, 15, 19} {
		if msg, _ := ci.MaybeInject(iter); msg != nil {
			t.Errorf("unexpected checkpoint at iteration %d", iter)
		}
	}
}

func TestCheckpointInjectorDisabled(t *testing.T) {
	ci := NewCheckpointInjector(0)
	for _, iter := range []int{10, 20, 25, 30, 40} {
		if msg, _ := ci.MaybeInject(iter); msg != nil {
			t.Errorf("checkpoint should be disabled, fired at iteration %d", iter)
		}
	}
}

func TestCheckpointInjectorCustomInterval(t *testing.T) {
	ci := NewCheckpointInjector(5)
	if msg, _ := ci.MaybeInject(5); msg == nil {
		t.Error("expected checkpoint at iteration 5 with interval=5")
	}
	if msg, _ := ci.MaybeInject(4); msg != nil {
		t.Error("unexpected checkpoint at iteration 4")
	}
}
