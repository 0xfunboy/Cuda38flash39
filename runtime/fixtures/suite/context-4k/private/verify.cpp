#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "policy.hpp"
#include <map>
struct Expected { const char* route; int factor, base, quota, rebate, ceiling; };
int main(){
    const Expected expected[]={
        {"/service_000",2,7,3,1,200},
        {"/service_001",3,24,14,2,219},
        {"/service_002",4,41,25,3,238},
        {"/service_003",5,58,36,4,257},
        {"/service_004",6,75,47,5,276},
        {"/service_005",7,92,58,6,295},
        {"/service_006",8,8,69,7,314},
    };
    auto policies=registry();
    CHECK(policies.size()==7);
    std::map<std::string,Policy> routes;
    std::string previous;
    for(auto p:policies){CHECK(previous.empty()||previous<p.route);CHECK(routes.emplace(p.route,p).second);previous=p.route;}
    for(auto e:expected){
        CHECK(routes.count(e.route)==1);auto fn=routes.at(e.route).evaluate;
        CHECK(fn({-1,false})==-2);CHECK(fn({e.quota+1,true})==-1);
        for(bool privileged:{false,true})for(int units:{0,1,e.quota}){
            int unit=e.factor-(privileged?e.rebate:0);if(unit<0)unit=0;
            std::int64_t bill=e.base+static_cast<std::int64_t>(unit)*units;if(bill>e.ceiling)bill=e.ceiling;
            CHECK(fn({units,privileged})==bill);
        }
    }
    std::cout<<"PASS service-registry: 7 distinct routes, 56 behavioral probes\n";
}
