module example.com/replaceapp

go 1.24

require example.com/greeter v0.0.0

require github.com/google/uuid v1.6.0 // indirect

replace example.com/greeter => ../greeter
