## ZenIO for zmq and nanomsg in Go

Package zenio implements ØMQ (ZMTP) and nanomsg (SP) in Go without any
dependencies on zeromq or nanomsg.

This is a work in progress and is considered *very experimental*. Expect it to
change or break in unexpected ways. Use at own risk.

## Installing

### Using *go get*

    $ go get github.com/op/zenio

After this command *zenio* is ready to use. Its source will be in:

    $GOROOT/src/pkg/github.com/op/zenio

You can use `go get -u -a` to update all installed packages.

## Documentation

For docs, see http://godoc.org/github.com/op/zenio or run:

    $ go doc github.com/op/zenio
