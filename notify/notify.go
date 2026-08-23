package notify

func Emit[L any](listeners []any, notify func(listener L)) {
	for i := range listeners {
		listener, ok := listeners[i].(L)
		if ok {
			notify(listener)
		}
	}
}
