#include "schedule.hpp"
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
        cursor=b;
    }
    if(cursor<work.second) result.emplace_back(cursor,work.second);
    return result;
}
