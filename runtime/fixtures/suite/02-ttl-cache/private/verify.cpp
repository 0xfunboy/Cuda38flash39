#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "cache.hpp"
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
