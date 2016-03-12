package fake

type List []interface{}
type Map map[string]interface{}

type Call struct {
	Arguments    List
	ReturnValues List
	SideEffects  Map
}

type Calls struct {
	calls []Call
}

func (c *Calls) Add(arguments, returnValues List, sideEffects Map) {
	c.calls = append(c.calls, Call{
		Arguments:    arguments,
		ReturnValues: returnValues,
		SideEffects:  sideEffects,
	})
}

func (c *Calls) Count() int {
	return len(c.calls)
}

func (c *Calls) NthCall(n int) *Call {
	if n > 0 && n <= len(c.calls) {
		return &c.calls[n-1]
	}
	return nil
}
