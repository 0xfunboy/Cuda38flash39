#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "jobs.hpp"
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
