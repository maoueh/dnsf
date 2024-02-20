## DNSF(aker)

A simple DNS server built on top of https://github.com/miekg/dns which aims at loading a single file defining the DNS records.

The server reads all the records and infers which zone it manages by collecting all unique zones and sub-zones.

The server then serves queries for those answer questions with records that matches the type.

> [!NOTE]
> **Caveats** Only one question is supported per query for now.

Here the copy of [simple.zone](./simple.zone) file:

```
$ORIGIN matt.local.     ; designates the start of this zone file in the namespace
$TTL 3600                ; default expiration time (in seconds) of all RRs without their own TTL value
matt.local.  IN  SOA   ns.matt.local. username.matt.local. ( 2020091025 7200 3600 1209600 3600 )
matt.local.  IN  NS    ns
matt.local.  IN  A     127.0.0.1
ns            IN  A     127.0.0.1
workers            IN  A     12.0.0.2
workers            IN  A     12.0.0.3
```

> [!NOTE]
> The `SOA` records is required and used for providing authority information on DNS answer

### Install

```bash
go install github.com/maoueh/dnsf@latest
```

### Run

```bash
# Listen on 8053 by default, pass port as second argument to change it
dnsf run simple.zone
```

### Test

```bash
dig -p 8053 @127.0.0.1 matt.local A
dig -p 8053 @127.0.0.1 workers.matt.local A
```
