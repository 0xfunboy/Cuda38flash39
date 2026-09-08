// Regression specifications. Golden code and hidden tests are never prompt files.
// generate.mjs materializes only buggy files under each task's repo directory.
export const harness = String.raw`#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
`;

export const fixtures = [
  {
    id: '01-escaped-settings', category: 'parser',
    task: 'Fix the settings parser regression. Preserve the public API. The configuration format is semicolon-separated key=value pairs; backslash escapes only semicolon, equals or backslash in either field. Exactly the first unescaped equals separates key/value. Empty input is an empty map; values may be empty. Reject empty keys, duplicate keys, dangling or unknown escapes, missing equals, empty pairs and trailing separators with std::invalid_argument. Preserve bytes and whitespace. Make existing and independent tests pass; do not change the API contract.',
    files: {
      'include/settings.hpp': '#pragma once\n#include <map>\n#include <string>\nstd::map<std::string,std::string> parse_pairs(const std::string& input);\n',
      'src/settings.cpp': String.raw`#include "settings.hpp"
#include <stdexcept>
std::map<std::string,std::string> parse_pairs(const std::string& input) {
    std::map<std::string,std::string> result;
    if (input.empty()) return result;
    std::string key, value;
    bool in_value = false, escaped = false;
    auto emit = [&] {
        if (!in_value || key.empty() || !result.emplace(key, value).second)
            throw std::invalid_argument("invalid pair");
        key.clear(); value.clear(); in_value = false;
    };
    for (char c : input) {
        if (escaped) {
            if (c != ';' && c != '=' && c != '\\') throw std::invalid_argument("unknown escape");
            (in_value ? value : key).push_back(c); escaped = false;
        } else if (c == '\\') {
            escaped = true;
        } else if (c == ';') {
            emit();
        } else if (c == '=' && !in_value) {
            in_value = true;
        } else {
            (in_value ? value : key).push_back(c);
        }
    }
    if (escaped) throw std::invalid_argument("dangling escape");
    emit();
    return result;
}
`,
    },
    injection: ['src/settings.cpp', "if (escaped) {", "if (escaped && c != ';') {"],
    test: String.raw`#include "settings.hpp"
int main() {
    CHECK(parse_pairs("").empty());
    CHECK(parse_pairs("x=1;y=2").at("y") == "2");
    CHECK(parse_pairs(R"(a=1\;2)").at("a") == "1;2");
    CHECK(parse_pairs(R"(\;\==\;\=)").at(";=") == ";=");
    CHECK(parse_pairs(R"(slash=\\;eq=a=b)").at("slash") == "\\");
    CHECK(parse_pairs(" spaced =  ").at(" spaced ") == "  ");
    CHECK(parse_pairs("empty=").at("empty").empty());
    for (auto s : {"=a", "a", "a=1;a=2", "a=1;", ";a=1", "a=1;;b=2", "a=\\q", "a=\\"}) CHECK(invalid([&] { parse_pairs(s); }));
    for (int i=0; i<100; ++i) {
        std::string v=std::to_string(i)+";=\\end", encoded;
        for(char c:v) { if(c==';'||c=='='||c=='\\') encoded+='\\'; encoded+=c; }
        CHECK(parse_pairs("k="+encoded).at("k")==v);
    }
    std::cout << "PASS escaped-settings: 115 cases\n";
}
`, cases: 115,
  },
  {
    id: '02-ttl-cache', category: 'mutable-state-boundary',
    task: 'Repair the expiring LRU cache without changing its API. Each put records a value expiring at now+ttl; zero/negative TTL removes the key. A key is expired when now >= expires_at. get returns nullopt on absent/expired entries and otherwise marks the item most recently used. put updates value, expiry and recency; capacity is enforced over live entries and expired entries must be discarded before eviction. A zero-capacity cache never stores values. All calls in a sequence have nondecreasing time. Timestamps and TTL in tests fit signed 64-bit without addition overflow.',
    files: {
      'include/cache.hpp': String.raw`#pragma once
#include <cstdint>
#include <list>
#include <optional>
#include <string>
#include <unordered_map>
class Cache {
    struct Entry { std::string value; std::int64_t expires; std::list<std::string>::iterator order; };
    std::size_t capacity;
    std::list<std::string> lru;
    std::unordered_map<std::string,Entry> entries;
    void purge(std::int64_t now);
public:
    explicit Cache(std::size_t limit): capacity(limit) {}
    void put(const std::string& key, std::string value, std::int64_t now, std::int64_t ttl);
    std::optional<std::string> get(const std::string& key, std::int64_t now);
};
`,
      'src/cache.cpp': String.raw`#include "cache.hpp"
void Cache::purge(std::int64_t now) {
    for (auto it=entries.begin(); it!=entries.end();) {
        if (it->second.expires <= now) { lru.erase(it->second.order); it=entries.erase(it); }
        else ++it;
    }
}
void Cache::put(const std::string& key, std::string value, std::int64_t now, std::int64_t ttl) {
    purge(now);
    auto old=entries.find(key);
    if(old!=entries.end()) { lru.erase(old->second.order); entries.erase(old); }
    if(ttl<=0 || capacity==0) return;
    while(entries.size()>=capacity) { entries.erase(lru.back()); lru.pop_back(); }
    lru.push_front(key);
    entries.emplace(key, Entry{std::move(value),now+ttl,lru.begin()});
}
std::optional<std::string> Cache::get(const std::string& key,std::int64_t now) {
    purge(now);
    auto it=entries.find(key);
    if(it==entries.end()) return std::nullopt;
    lru.splice(lru.begin(),lru,it->second.order);
    return it->second.value;
}
`,
    }, injection: ['src/cache.cpp', 'it->second.expires <= now', 'it->second.expires < now'],
    test: String.raw`#include "cache.hpp"
int main() {
    Cache c(2); c.put("a","A",0,5); c.put("b","B",0,20);
    CHECK(c.get("a",4)=="A"); CHECK(!c.get("a",5));
    c.put("c","C",5,20); CHECK(c.get("b",5)=="B");
    c.put("d","D",5,20); CHECK(!c.get("c",5)); CHECK(c.get("b",5)=="B");
    c.put("b","B2",5,30); CHECK(c.get("b",20)=="B2"); CHECK(!c.get("b",35));
    Cache zero(0); zero.put("x","v",0,3); CHECK(!zero.get("x",0));
    Cache e(1); e.put("k","v",0,10); e.put("k","bad",1,0); CHECK(!e.get("k",1));
    for(int i=0;i<100;++i) { Cache q(1); q.put("x","v",100,i); CHECK(!q.get("x",100+i)); }
    std::cout << "PASS ttl-cache: 109 cases\n";
}
`, cases:109,
  },
  {
    id:'03-schedule-difference', category:'algorithm',
    task:'Fix the scheduling library: subtract every blocked half-open interval from a single half-open working interval. Blocked intervals can be unsorted, overlapping, nested, duplicated, empty or outside the working interval. Return ordered, nonempty, disjoint remaining intervals; touching blocks leave no gap. Reject any interval with start>end using std::invalid_argument, including invalid blocked intervals when the working interval is empty. Empty intervals otherwise do nothing. Preserve the public API and signed 64-bit endpoints.',
    files:{
      'include/schedule.hpp':'#pragma once\n#include <cstdint>\n#include <utility>\n#include <vector>\nusing Interval=std::pair<std::int64_t,std::int64_t>;\nstd::vector<Interval> available(Interval work,std::vector<Interval> blocked);\n',
      'src/schedule.cpp':String.raw`#include "schedule.hpp"
#include <algorithm>
#include <stdexcept>
std::vector<Interval> available(Interval work,std::vector<Interval> blocked) {
    if(work.first>work.second) throw std::invalid_argument("reversed work");
    for(auto x:blocked) if(x.first>x.second) throw std::invalid_argument("reversed block");
    std::sort(blocked.begin(),blocked.end());
    std::vector<Interval> result;
    auto cursor=work.first;
    for(auto [a,b]:blocked) {
        a=std::max(a,work.first); b=std::min(b,work.second);
        if(a>=b) continue;
        if(cursor<a) result.emplace_back(cursor,a);
        cursor=std::max(cursor,b);
    }
    if(cursor<work.second) result.emplace_back(cursor,work.second);
    return result;
}
`,
    },injection:['src/schedule.cpp','cursor=std::max(cursor,b);','cursor=b;'],
    test:String.raw`#include "schedule.hpp"
int main() {
    CHECK((available({0,10},{{1,8},{2,3}})==std::vector<Interval>{{0,1},{8,10}}));
    CHECK((available({0,10},{{7,12},{-5,3},{3,7}}).empty()));
    CHECK((available({-10,10},{{0,0},{-15,-12},{15,20}})==std::vector<Interval>{{-10,10}}));
    CHECK(invalid([]{available({1,1},{{2,1}});}));
    CHECK(invalid([]{available({2,1},{});}));
    // Independent unit-cell oracle, not a second interval-subtraction implementation.
    for(int seed=0;seed<160;++seed) {
        std::vector<Interval> blocks;
        for(int j=0;j<6;++j) { int a=(seed*7+j*11)%31-10,b=(seed*13+j*5)%31-10; if(a>b)std::swap(a,b); blocks.emplace_back(a,b); }
        auto answer=available({-5,15},blocks);
        for(int p=-5;p<15;++p) {
            bool free=true; for(auto [a,b]:blocks) if(a<=p && p<b) free=false;
            int copies=0; for(auto [a,b]:answer) if(a<=p&&p<b) ++copies;
            CHECK(copies==(free?1:0));
        }
        for(std::size_t i=0;i<answer.size();++i) { CHECK(answer[i].first<answer[i].second); if(i)CHECK(answer[i-1].second<answer[i].first); }
    }
    std::cout << "PASS schedule-difference: 165 scenarios\n";
}
`,cases:165,
  },
  {
    id:'04-inventory-import',category:'error-transaction',
    task:'Repair the inventory import API. import_inventory accepts LF-separated name,nonnegative_decimal_quantity records; an empty input is a no-op and one final LF is allowed. Names consist of ASCII letters, digits or underscore, nonempty. Quantities must contain only digits, range 0..1000000; repeated records add to current inventory and the resulting quantity must stay <=1000000. No whitespace/sign/suffix is accepted. A malformed record or overflow must throw std::invalid_argument and leave the entire inventory unchanged, including updates from earlier lines. Preserve existing entries and public API.',
    files:{
      'include/inventory.hpp':'#pragma once\n#include <map>\n#include <string>\nusing Inventory=std::map<std::string,int>;\nvoid import_inventory(Inventory& inventory,const std::string& csv);\n',
      'src/inventory.cpp':String.raw`#include "inventory.hpp"
#include <charconv>
#include <stdexcept>
void import_inventory(Inventory& inventory,const std::string& csv) {
    Inventory next=inventory;
    std::size_t start=0;
    while(start<csv.size()) {
        auto end=csv.find('\n',start); if(end==std::string::npos)end=csv.size();
        std::string line=csv.substr(start,end-start);
        auto comma=line.find(',');
        if(comma==std::string::npos || comma==0)throw std::invalid_argument("record");
        std::string name=line.substr(0,comma), digits=line.substr(comma+1);
        for(char c:name)if(!((c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='_'))throw std::invalid_argument("name");
        if(digits.empty())throw std::invalid_argument("quantity");
        for(char c:digits)if(c<'0'||c>'9')throw std::invalid_argument("quantity");
        int quantity=0;
        auto parsed=std::from_chars(digits.data(),digits.data()+digits.size(),quantity);
        if(parsed.ec!=std::errc{} || parsed.ptr!=digits.data()+digits.size() || quantity>1000000)throw std::invalid_argument("range");
        int previous=next.count(name)?next.at(name):0;
        if(quantity>1000000-previous)throw std::invalid_argument("overflow");
        next[name]=previous+quantity;
        start=end+1;
    }
    inventory=std::move(next);
}
`,
    },injection:['src/inventory.cpp','next[name]=previous+quantity;','next[name]=previous+quantity; inventory=next;'],
    test:String.raw`#include "inventory.hpp"
int main() {
    Inventory inv{{"apple",4}}; import_inventory(inv,"apple,2\npear,0003\n"); CHECK(inv.at("apple")==6&&inv.at("pear")==3);
    const Inventory original=inv;
    for(auto bad:{"new,7\nbad,-1", "new,7\nbad,2x", "new,7\n", "bad name,1", "x,1000001", "x,999999999999999999999", "x,+1", "x, 1", "x,1\n\n"}) {
        // The single trailing LF case is valid and is checked independently below.
        if(std::string(bad)=="new,7\n")continue;
        inv=original; CHECK(invalid([&]{import_inventory(inv,bad);})); CHECK(inv==original);
    }
    inv={{"x",999999}}; CHECK(invalid([&]{import_inventory(inv,"y,5\nx,2");})); CHECK((inv==Inventory{{"x",999999}}));
    import_inventory(inv,""); CHECK(inv.at("x")==999999);
    import_inventory(inv,"new,7\n");CHECK(inv.at("new")==7);
    std::cout<<"PASS inventory-import: 12 scenarios\n";
}
`,cases:12,
  },
  {
    id:'05-price-cache-refactor',category:'multi-file-cache',
    task:'Fix the pricing repository regression after a refactor. Catalog owns current nonnegative integer-cent prices and a monotonically increasing revision. Every set_price changes the revision, even an update of the same SKU. InvoiceService quotes a cart by summing current catalog unit prices times nonnegative quantities; totals fit signed 64-bit. Unknown SKUs throw std::out_of_range and negative quantities throw std::invalid_argument. Cached quotes must never outlive catalog changes. Repeated items and ordering do not change the mathematical sum. Preserve the three public interfaces and the cache (do not remove caching).',
    files:{
      'include/catalog.hpp':String.raw`#pragma once
#include <cstdint>
#include <map>
#include <string>
class Catalog {
    std::map<std::string,std::int64_t> prices_;
    std::uint64_t revision_=0;
public:
    void set_price(const std::string& sku,std::int64_t cents);
    std::int64_t price(const std::string& sku) const;
    std::uint64_t revision() const { return revision_; }
};
`,
      'include/invoice.hpp':String.raw`#pragma once
#include "catalog.hpp"
#include <utility>
#include <vector>
using Cart=std::vector<std::pair<std::string,int>>;
class InvoiceService {
    const Catalog& catalog_;
    std::uint64_t revision_=0;
    std::map<Cart,std::int64_t> cache_;
public:
    explicit InvoiceService(const Catalog& catalog):catalog_(catalog),revision_(catalog.revision()){}
    std::int64_t quote(const Cart& cart);
};
`,
      'src/catalog.cpp':String.raw`#include "catalog.hpp"
#include <stdexcept>
void Catalog::set_price(const std::string& sku,std::int64_t cents) {
    if(cents<0)throw std::invalid_argument("negative price");
    prices_[sku]=cents;
    ++revision_;
}
std::int64_t Catalog::price(const std::string& sku)const {return prices_.at(sku);}
`,
      'src/invoice.cpp':String.raw`#include "invoice.hpp"
#include <stdexcept>
std::int64_t InvoiceService::quote(const Cart& cart) {
    if(revision_!=catalog_.revision()) {cache_.clear();revision_=catalog_.revision();}
    auto cached=cache_.find(cart);if(cached!=cache_.end())return cached->second;
    std::int64_t total=0;
    for(const auto& [sku,n]:cart) {if(n<0)throw std::invalid_argument("quantity");total+=catalog_.price(sku)*n;}
    cache_[cart]=total;
    return total;
}
`,
    },injection:['src/catalog.cpp','prices_[sku]=cents;\n    ++revision_;','auto inserted=prices_.emplace(sku,cents);\n    if(inserted.second)++revision_;\n    else inserted.first->second=cents;'],
    test:String.raw`#include "invoice.hpp"
int main(){
    Catalog c;c.set_price("a",100);c.set_price("b",30);InvoiceService invoice(c);Cart cart{{"a",2},{"b",3}};
    CHECK(invoice.quote(cart)==290);CHECK(invoice.quote(cart)==290);
    auto version=c.revision();c.set_price("a",120);CHECK(c.revision()==version+1);CHECK(invoice.quote(cart)==330);
    c.set_price("a",120);CHECK(c.revision()==version+2);CHECK(invoice.quote(cart)==330);
    c.set_price("a",0);CHECK(invoice.quote(cart)==90);CHECK(invoice.quote({})==0);
    CHECK(invoice.quote({{"b",1},{"b",2}})==90);
    CHECK(invalid([&]{invoice.quote({{"a",-1}});}));CHECK(invalid([&]{c.set_price("x",-1);}));
    bool missing=false;try{invoice.quote({{"absent",1}});}catch(const std::out_of_range&){missing=true;}CHECK(missing);
    for(int n=0;n<80;++n){c.set_price("b",n);CHECK(invoice.quote(cart)==3*n);}
    std::cout<<"PASS price-cache-refactor: 92 checks\n";
}
`,cases:92,
  },
  {
    id:'06-job-acknowledgement',category:'multi-file-state-error',
    task:'Repair the durable-job coordinator state transitions. Queue::lease returns the first pending job and marks it in flight; exhausted returns nullopt. Worker::run_one returns false if no job exists. Otherwise it calls the supplied handler once. Success acknowledges the job and returns true; a handler exception must release the same job to pending and propagate the original exception. Queue::ack/release only accept an in-flight existing ID and otherwise throw std::logic_error without changing state. Completed jobs must not run again; failed jobs must be available to retry. Preserve public interfaces and FIFO ordering by insertion.',
    files:{
      'include/jobs.hpp':String.raw`#pragma once
#include <functional>
#include <optional>
#include <string>
#include <vector>
struct Job{int id;std::string payload;};
class Queue {
    enum class State{pending,inflight,done};
    struct Item{Job job;State state;};
    std::vector<Item> items_;
public:
    void add(Job job);
    std::optional<Job> lease();
    void ack(int id);
    void release(int id);
};
class Worker {
    Queue& queue_;
public:
    explicit Worker(Queue& q):queue_(q){}
    bool run_one(const std::function<void(const Job&)>& handler);
};
`,
      'src/queue.cpp':String.raw`#include "jobs.hpp"
#include <stdexcept>
void Queue::add(Job job){for(auto& x:items_)if(x.job.id==job.id)throw std::logic_error("duplicate");items_.push_back({std::move(job),State::pending});}
std::optional<Job> Queue::lease(){for(auto& x:items_)if(x.state==State::pending){x.state=State::inflight;return x.job;}return std::nullopt;}
void Queue::ack(int id){for(auto& x:items_)if(x.job.id==id&&x.state==State::inflight){x.state=State::done;return;}throw std::logic_error("not in flight");}
void Queue::release(int id){for(auto& x:items_)if(x.job.id==id&&x.state==State::inflight){x.state=State::pending;return;}throw std::logic_error("not in flight");}
`,
      'src/worker.cpp':String.raw`#include "jobs.hpp"
bool Worker::run_one(const std::function<void(const Job&)>& handler) {
    auto job=queue_.lease();if(!job)return false;
    try {handler(*job);} catch (...) {queue_.release(job->id);throw;}
    queue_.ack(job->id);
    return true;
}
`,
    },injection:['src/worker.cpp','queue_.release(job->id);throw;','queue_.ack(job->id);throw;'],
    test:String.raw`#include "jobs.hpp"
int main(){
    Queue q;q.add({1,"first"});q.add({2,"second"});Worker w(q);int calls=0;bool caught=false;
    try{w.run_one([&](const Job& j){CHECK_UNUSED: ++calls; if(j.id!=1)std::abort();throw std::runtime_error("temporary");});}catch(const std::runtime_error& e){caught=std::string(e.what())=="temporary";}
    CHECK(caught&&calls==1);
    int id=0;CHECK(w.run_one([&](const Job& j){id=j.id;}));CHECK(id==1);
    CHECK(w.run_one([&](const Job& j){id=j.id;}));CHECK(id==2);
    CHECK(!w.run_one([&](const Job&){std::abort();}));
    int rejected=0;try{q.ack(1);}catch(const std::logic_error&){++rejected;}try{q.release(99);}catch(const std::logic_error&){++rejected;}CHECK(rejected==2);
    Queue a;a.add({7,"retry"});auto leased=a.lease();CHECK(leased&&leased->id==7);CHECK(!a.lease());a.release(7);CHECK(a.lease()->id==7);a.ack(7);CHECK(!a.lease());
    std::cout<<"PASS job-acknowledgement: 12 checks\n";
}
`,cases:12,
  },
];
