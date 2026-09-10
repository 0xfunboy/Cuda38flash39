#include "inventory.hpp"
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
