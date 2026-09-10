#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "schedule.hpp"
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
