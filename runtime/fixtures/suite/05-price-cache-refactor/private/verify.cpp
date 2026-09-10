#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "invoice.hpp"
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
