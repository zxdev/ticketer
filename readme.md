# ticketer


Generate new ticket with a uuid based file manager; concurrency safe

random   :0  e4e45937-79c9-c3b4-07e4-7c13d989f9235e15
sequence :1+ 00000001-5d9b-95d2-de8d-9c7cb21451fac9c1

[4]byte header, uint32 identifier
[2]byte high 32bit unix time
[2]byte low 32bit unix time
[2]byte random uint16
[8]byte random uint64



