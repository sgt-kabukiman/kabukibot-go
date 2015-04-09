package twitch

// a listener function is just any function (so, actually, `func(msg interface{})`)
type listenerFunc interface{}

// the struct returned when adding new listeners;
// do not store a reference to the list in which this listeners is placed,
// as the list moved around when listeners are removed.
type Listener struct {
	dispatcher *dispatcher
	callback   listenerFunc
	event      string
	id         int
}

// maps events to lists of listeners
type listenerMap map[string][]Listener

// the exported interface to the dispatching
type Dispatcher interface {
	AddListener(string, *Channel, listenerFunc) *Listener

	TriggerEvent(string, *Channel, walker)

	OnTextMessage(TextHandlerFunc, *Channel)     *Listener
	OnTwitchMessage(TwitchHandlerFunc, *Channel) *Listener
	OnJoin(JoinHandlerFunc, *Channel)            *Listener
	OnPart(JoinHandlerFunc, *Channel)            *Listener

	HandleTextMessage(TextMessage)
	HandleTwitchMessage(TwitchMessage)
	HandleJoin(*Channel)
	HandlePart(*Channel)
}

type triggerQueueItem struct {
	event   string
	channel *Channel
	visitor walker
}

type dispatcher struct {
	listeners    listenerMap
	listenerID   int // increments with each new listener being added
	lock         bool
	triggerQueue []triggerQueueItem
}

func (self *Listener) Remove() {
	if self.dispatcher == nil {
		return
	}

	list, exists := self.dispatcher.listeners[self.event]

	if !exists {
		self.dispatcher = nil
		return
	}

	// find listener
	pos := -1

	for idx, listener := range list {
		if self.Equals(&listener) {
			pos = idx
			break
		}
	}

	if pos != -1 {
		self.dispatcher.listeners[self.event] = append(list[:pos], list[(pos+1):]...)
		self.dispatcher = nil
	}
}

func (l *Listener) Equals(m *Listener) bool {
	return l.id == m.id
}

type walker func(interface{})

func NewDispatcher() Dispatcher {
	return &dispatcher{make(listenerMap), 0, false, make([]triggerQueueItem, 0)}
}

func (d *dispatcher) OnTextMessage(f TextHandlerFunc, c *Channel)     *Listener { return d.AddListener("TEXT", c, f)   }
func (d *dispatcher) OnTwitchMessage(f TwitchHandlerFunc, c *Channel) *Listener { return d.AddListener("TWITCH", c, f) }
func (d *dispatcher) OnJoin(f JoinHandlerFunc, c *Channel)            *Listener { return d.AddListener("JOIN", c, f)   }
func (d *dispatcher) OnPart(f JoinHandlerFunc, c *Channel)            *Listener { return d.AddListener("PART", c, f)   }

func (d *dispatcher) HandleTextMessage(msg TextMessage) {
	d.TriggerEvent("TEXT", msg.Channel(), func(listener interface{}) {
		listener.(TextHandlerFunc)(msg)
	})
}

func (d *dispatcher) HandleTwitchMessage(msg TwitchMessage) {
	d.TriggerEvent("TWITCH", msg.Channel(), func(listener interface{}) {
		listener.(TwitchHandlerFunc)(msg)
	})
}

func (d *dispatcher) HandleJoin(c *Channel) {
	d.TriggerEvent("JOIN", c, func(listener interface{}) {
		listener.(JoinHandlerFunc)(c)
	})
}

func (d *dispatcher) HandlePart(c *Channel) {
	d.TriggerEvent("PART", c, func(listener interface{}) {
		listener.(JoinHandlerFunc)(c)
	})
}

func (d *dispatcher) AddListener(event string, c *Channel, f listenerFunc) *Listener {
	fullEventName := event

	if c != nil {
		fullEventName = fullEventName + "#" + c.Name
	}

	// build our listener
	listener := Listener{d, f, fullEventName, d.listenerID}

	// find the listener list for this event
	list, exists := d.listeners[fullEventName]

	if !exists {
		list = make([]Listener, 0)
	} else {
		// check if this listener is already in the list
		for _, i := range list {
			if i.Equals(&listener) {
				return &i
			}
		}
	}

	d.listeners[fullEventName] = append(list, listener)
	d.listenerID               = d.listenerID + 1

	return &listener
}

func (d *dispatcher) TriggerEvent(event string, c *Channel, visitor walker) {
	// put this trigger request on the queue of events to churn
	d.triggerQueue = append(d.triggerQueue, triggerQueueItem{event, c, visitor})

	// if we are already working on the trigger queue in another stack level,
	// quit and let us return to that at a later time.
	if (d.lock) {
		return
	}

	d.lock = true

	// execute event listeners for the current event and then continue to
	// execute all piled up triggers that are fired by the listeners.

	for len(d.triggerQueue) > 0 {
		// pop the first item of the queue
		item          := d.triggerQueue[0]
		d.triggerQueue = d.triggerQueue[1:]

		// trigger all listeners for the channel-less case ("message")
		d.runListeners(item.event, item.visitor)

		if item.channel != nil {
			d.runListeners(item.event + "#" + item.channel.Name, item.visitor)
		}
	}

	// release lock again, so the next call will start to working on the queue
	d.lock = false
}

// private helpers

func (d *dispatcher) runListeners(event string, visitor walker) {
	l, exists := d.listeners[event]

	if !exists {
		return
	}

	for _, listener := range l {
		visitor(listener.callback)
	}
}
